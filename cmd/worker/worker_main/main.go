package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"jobqueue/internal/config"
	"jobqueue/internal/jobs"
	"jobqueue/internal/queue"
	"jobqueue/proto/workerpb"
)

const maxAttempts = 3

func main() {
	cfg := config.Load()

	workerID := uuid.NewString()
	// Use cfg.WorkerDispatchAddress() so WORKER_HOST is respected in Docker
	// (resolves to "worker:<port>") while still defaulting to "localhost:<port>"
	// when running locally.
	dispatchAddr := cfg.WorkerDispatchAddress()

	// -------------------------------------------------------------------------
	// Connect to RabbitMQ
	// -------------------------------------------------------------------------
	rmq, err := queue.NewRabbitMQ(&queue.Config{
		URL:       cfg.RabbitMQURL,
		QueueName: cfg.RabbitMQQueue,
	})
	if err != nil {
		log.Fatalf("failed to connect to RabbitMQ: %v", err)
	}

	// -------------------------------------------------------------------------
	// HTTP dispatch server
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

	httpServer := &http.Server{
		Addr:    cfg.DispatchAddr(),
		Handler: mux,
	}

	go func() {
		log.Printf("HTTP dispatch server listening on %s", cfg.DispatchAddr())
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("dispatch server error: %v", err)
		}
	}()

	// -------------------------------------------------------------------------
	// Connect to API gRPC and register
	// -------------------------------------------------------------------------
	conn, err := grpc.NewClient(
		cfg.APIGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("failed to connect to API gRPC: %v", err)
	}

	apiClient := workerpb.NewWorkerServiceClient(conn)

	// Register with format "uuid@host:port" so the API can store the dispatch address.
	registrationID := workerID + "@" + dispatchAddr
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
	// RabbitMQ consumer
	// -------------------------------------------------------------------------
	if err := rmq.Channel().Qos(1, 0, false); err != nil {
		log.Fatalf("failed to set QoS: %v", err)
	}

	msgs, err := rmq.Channel().Consume(
		cfg.RabbitMQQueue,
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

	// -------------------------------------------------------------------------
	// Graceful shutdown
	// -------------------------------------------------------------------------
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for msg := range msgs {
			currentLoad = 1
			handleMessage(msg, rmq)
			currentLoad = 0
		}
	}()

	<-quit
	log.Println("Shutting down worker...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	httpServer.Shutdown(ctx)
	rmq.Close()
	conn.Close()

	log.Println("Worker stopped cleanly")
}

func handleMessage(msg amqp.Delivery, rmq *queue.RabbitMQ) {
	var job jobs.Job

	if err := json.Unmarshal(msg.Body, &job); err != nil {
		log.Println("invalid job payload, discarded:", err)
		// Malformed message — Nack without requeue so it routes to DLQ via DLX.
		_ = msg.Nack(false, false)
		return
	}

	log.Printf("[queue] job received: id=%s type=%s attempt=%d", job.ID, job.Type, job.Attempts)

	// Simulate job processing — replace this with real handler logic.
	jobSucceeded := true // TODO: run the actual job here

	if jobSucceeded {
		log.Printf("[queue] job %s succeeded on attempt %d", job.ID, job.Attempts)
		_ = msg.Ack(false)
		return
	}

	// Job failed — decide whether to retry or send to DLQ.
	job.Attempts++
	if job.Attempts < maxAttempts {
		log.Printf("[queue] job %s failed, retrying (%d/%d)", job.ID, job.Attempts, maxAttempts)

		data, err := json.Marshal(job)
		if err != nil {
			log.Println("failed to re-marshal job, discarding:", err)
			_ = msg.Nack(false, false)
			return
		}

		// Ack the original message and re-publish with the incremented attempt
		// counter so retries are tracked correctly.
		_ = msg.Ack(false)
		if err := rmq.Publish(data); err != nil {
			log.Printf("failed to re-publish job %s: %v", job.ID, err)
		}
		return
	}

	// Exhausted all attempts — Nack without requeue so the DLX routes it to DLQ.
	log.Printf("[queue] job %s exhausted all %d attempts — sending to DLQ", job.ID, maxAttempts)
	_ = msg.Nack(false, false)
}
