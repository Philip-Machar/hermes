package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
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
	// --port lets you run multiple workers locally without conflicts.
	// e.g. go run ./cmd/worker/worker_main/main.go --port 9002
	dispatchPort := flag.Int("port", 9001, "HTTP port this worker listens on for direct job dispatch")
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
	// Start HTTP dispatch server
	// The API will POST jobs directly to /dispatch on this port.
	// -------------------------------------------------------------------------
	mux := http.NewServeMux()
	mux.HandleFunc("/dispatch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			JobID   string      `json:"job_id"`
			JobType string      `json:"job_type"`
			Payload interface{} `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}

		log.Printf("[direct] job received: id=%s type=%s", req.JobID, req.JobType)
		log.Printf("[direct] job %s completed", req.JobID)

		w.WriteHeader(http.StatusOK)
	})

	go func() {
		log.Printf("HTTP dispatch server listening on :%d", *dispatchPort)
		if err := http.ListenAndServe(fmt.Sprintf(":%d", *dispatchPort), mux); err != nil {
			log.Fatalf("dispatch server error: %v", err)
		}
	}()

	// -------------------------------------------------------------------------
	// Connect to API gRPC server and register
	// -------------------------------------------------------------------------
	conn, err := grpc.NewClient(
		"localhost:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("failed to connect to API gRPC: %v", err)
	}
	defer conn.Close()

	apiClient := workerpb.NewWorkerServiceClient(conn)

	// Register as "<uuid>@<dispatch-address>" so the API knows where to reach us
	registrationID := fmt.Sprintf("%s@%s", workerID, dispatchAddr)
	_, err = apiClient.Register(
		context.Background(),
		&workerpb.RegisterRequest{WorkerId: registrationID},
	)
	if err != nil {
		log.Fatalf("failed to register: %v", err)
	}
	log.Printf("Worker registered | id=%s | dispatch=http://%s", workerID, dispatchAddr)

	// -------------------------------------------------------------------------
	// Heartbeat loop
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
	// RabbitMQ consumer — handles queued (fallback) jobs
	// -------------------------------------------------------------------------
	if err := rmq.Channel().Qos(1, 0, false); err != nil {
		log.Fatalf("failed to set QoS: %v", err)
	}

	msgs, err := rmq.Channel().Consume(
		cfg.QueueName,
		"",
		false,
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
