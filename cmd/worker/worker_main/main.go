package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"jobqueue/internal/jobs"
	"jobqueue/internal/queue"
	"jobqueue/proto/workerpb"
)

const maxAttempts = 3

func main() {

	// --port lets you run multiple workers locally without port conflicts.
	// e.g. go run ./cmd/worker/worker_main/main.go --port 50053
	dispatchPort := flag.Int("port", 50052, "port this worker listens on for direct job dispatch")
	flag.Parse()

	workerID := uuid.NewString()
	dispatchAddr := fmt.Sprintf("localhost:%d", *dispatchPort)

	// -------------------------------------------------------------------------
	// Connect to RabbitMQ
	// -------------------------------------------------------------------------
	cfg := queue.Config{
		URL:       "amqp://guest:guest@localhost:5672/",
		QueueName: "jobs",
	}

	rmq, err := queue.NewRabbitMQ(&cfg)
	if err != nil {
		log.Fatalf("failed to connect to RabbitMQ: %v", err)
	}
	defer rmq.Close()

	// -------------------------------------------------------------------------
	// Start this worker's own WorkerDispatchService gRPC server.
	// The API will dial this address to push jobs directly to us.
	// -------------------------------------------------------------------------
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *dispatchPort))
	if err != nil {
		log.Fatalf("failed to listen on port %d: %v", *dispatchPort, err)
	}

	dispatchServer := grpc.NewServer()
	workerpb.RegisterWorkerDispatchServiceServer(dispatchServer, &dispatchHandler{workerID: workerID})

	go func() {
		log.Printf("WorkerDispatchService listening on :%d", *dispatchPort)
		if err := dispatchServer.Serve(lis); err != nil {
			log.Fatalf("dispatch gRPC server error: %v", err)
		}
	}()

	// -------------------------------------------------------------------------
	// Connect to the API's WorkerService gRPC server at :50051
	// -------------------------------------------------------------------------
	conn, err := grpc.NewClient(
		"localhost:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("failed to connect to API gRPC server: %v", err)
	}
	defer conn.Close()

	apiClient := workerpb.NewWorkerServiceClient(conn)

	// -------------------------------------------------------------------------
	// Register with the API.
	// Format: "<uuid>@<dispatch-address>" so the API stores our address.
	// -------------------------------------------------------------------------
	registrationID := fmt.Sprintf("%s@%s", workerID, dispatchAddr)
	_, err = apiClient.Register(
		context.Background(),
		&workerpb.RegisterRequest{WorkerId: registrationID},
	)
	if err != nil {
		log.Fatalf("failed to register with API: %v", err)
	}
	log.Printf("Worker registered | id=%s | dispatch=%s", workerID, dispatchAddr)

	// -------------------------------------------------------------------------
	// Heartbeat loop — sends load status every 5 seconds.
	// Uses plain workerID (no address) because the API already stored the address.
	// -------------------------------------------------------------------------
	var currentLoad int32 = 0

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			_, err := apiClient.Heartbeat(
				context.Background(),
				&workerpb.HeartbeatRequest{
					WorkerId: workerID,
					Load:     currentLoad,
				},
			)
			if err != nil {
				log.Println("heartbeat failed:", err)
				continue
			}
			log.Printf("heartbeat sent (load=%d)", currentLoad)
		}
	}()

	// -------------------------------------------------------------------------
	// RabbitMQ consumer — handles jobs that were queued as fallback
	// -------------------------------------------------------------------------
	if err := rmq.Channel().Qos(1, 0, false); err != nil {
		log.Fatalf("failed to set QoS: %v", err)
	}

	msgs, err := rmq.Channel().Consume(
		cfg.QueueName,
		"",    // auto-generated consumer tag
		false, // manual ack
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("failed to start consuming: %v", err)
	}

	log.Println("Worker ready — waiting for jobs")

	for msg := range msgs {
		currentLoad = 1
		handleMessage(msg, rmq)
		currentLoad = 0
	}
}

// -------------------------------------------------------------------------
// WorkerDispatchService implementation
// Receives jobs pushed directly from the API via gRPC.
// -------------------------------------------------------------------------

type dispatchHandler struct {
	workerpb.UnimplementedWorkerDispatchServiceServer
	workerID string
}

func (h *dispatchHandler) DispatchJob(ctx context.Context, req *workerpb.DispatchRequest) (*workerpb.DispatchResponse, error) {
	log.Printf("[direct] job received: id=%s type=%s", req.JobId, req.JobType)

	// Execute the job
	log.Printf("[direct] executing job %s", req.JobId)
	// Real work goes here. Currently: log and succeed immediately.
	log.Printf("[direct] job %s completed", req.JobId)

	return &workerpb.DispatchResponse{Status: "ok"}, nil
}

// -------------------------------------------------------------------------
// RabbitMQ message handler — processes queued (fallback) jobs
// -------------------------------------------------------------------------

func handleMessage(msg amqp.Delivery, rmq *queue.RabbitMQ) {
	var job jobs.Job

	if err := json.Unmarshal(msg.Body, &job); err != nil {
		log.Println("invalid job payload, discarded:", err)
		_ = msg.Nack(false, false)
		return
	}

	log.Printf("[queue] job received: id=%s type=%s attempt=%d", job.ID, job.Type, job.Attempts)

	if job.Attempts < maxAttempts-1 {
		job.Attempts++
		log.Printf("[queue] job %s retrying (%d/%d)", job.ID, job.Attempts, maxAttempts)

		data, err := json.Marshal(job)
		if err != nil {
			log.Println("failed to re-marshal job, discarding:", err)
			_ = msg.Nack(false, false)
			return
		}

		_ = msg.Nack(false, false)

		if err := rmq.Publish(data); err != nil {
			log.Println("failed to re-publish job:", err)
		}
		return
	}

	log.Printf("[queue] job %s succeeded", job.ID)
	_ = msg.Ack(false)
}
