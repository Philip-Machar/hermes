package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"jobqueue/internal/config"
	"jobqueue/internal/jobs"
	"jobqueue/internal/queue"
	"jobqueue/internal/worker"
	"jobqueue/proto/workerpb"
)

func main() {
	cfg := config.Load()

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
	defer rmq.Close()

	// -------------------------------------------------------------------------
	// Worker registry + cleanup ticker
	// -------------------------------------------------------------------------
	registry := worker.NewRegistry()

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			registry.CleanupExpired()
		}
	}()

	// -------------------------------------------------------------------------
	// gRPC server — workers Register and Heartbeat here
	// -------------------------------------------------------------------------
	grpcServer := grpc.NewServer()
	workerpb.RegisterWorkerServiceServer(grpcServer, worker.NewGRPCServer(registry))

	grpcLis, err := net.Listen("tcp", cfg.GRPCAddr())
	if err != nil {
		log.Fatalf("failed to listen on gRPC port: %v", err)
	}

	go func() {
		log.Printf("gRPC server listening on %s", cfg.GRPCAddr())
		if err := grpcServer.Serve(grpcLis); err != nil {
			log.Fatalf("gRPC server error: %v", err)
		}
	}()

	// -------------------------------------------------------------------------
	// HTTP API server
	// -------------------------------------------------------------------------
	mux := http.NewServeMux()

	// POST /jobs — submit a job
	mux.HandleFunc("/jobs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var body struct {
			Type    string      `json:"type"`
			Payload interface{} `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
			return
		}

		job := jobs.Job{
			ID:       uuid.NewString(),
			Type:     body.Type,
			Payload:  body.Payload,
			Attempts: 0,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)

		// Try direct dispatch to a live worker first.
		workerID := registry.PickWorker()
		if workerID != "" {
			if err := registry.DispatchJob(workerID, job.ID, job.Type, job.Payload); err == nil {
				log.Printf("job %s dispatched directly to worker %s", job.ID, workerID)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"job":       job,
					"routed":    "direct",
					"worker_id": workerID,
				})
				return
			}
			log.Printf("direct dispatch to worker %s failed, falling back to queue", workerID)
		}

		// No alive workers or dispatch failed — publish to RabbitMQ.
		data, err := json.Marshal(job)
		if err != nil {
			http.Error(w, "failed to encode job", http.StatusInternalServerError)
			return
		}
		if err := rmq.Publish(data); err != nil {
			http.Error(w, "failed to queue job", http.StatusInternalServerError)
			return
		}
		log.Printf("job %s queued (no alive workers)", job.ID)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"job":    job,
			"routed": "queue",
			"note":   "no alive workers, job queued",
		})
	})

	// GET /workers — live snapshot of the worker fleet
	mux.HandleFunc("/workers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		snapshot := registry.List()

		type workerView struct {
			WorkerID     string `json:"worker_id"`
			LastSeenUnix int64  `json:"last_seen_unix"`
			Load         int32  `json:"load"`
			Status       string `json:"status"`
		}

		workers := make([]workerView, 0, len(snapshot))
		for id, info := range snapshot {
			status := "idle"
			if info.Load > 0 {
				status = "busy"
			}
			workers = append(workers, workerView{
				WorkerID:     id,
				LastSeenUnix: info.LastSeen.Unix(),
				Load:         info.Load,
				Status:       status,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"count":   len(workers),
			"workers": workers,
		})
	})

	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr(),
		Handler: mux,
	}

	go func() {
		log.Printf("API listening on %s", cfg.HTTPAddr())
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// -------------------------------------------------------------------------
	// Graceful shutdown
	// -------------------------------------------------------------------------
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down API server...")
	grpcServer.GracefulStop()
	log.Println("API server stopped cleanly")
}
