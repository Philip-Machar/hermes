package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"jobqueue/internal/config"
	"jobqueue/internal/jobs"
	"jobqueue/internal/queue"
	"jobqueue/internal/worker"
	"jobqueue/proto/workerpb"

	"github.com/google/uuid"
	"google.golang.org/grpc"
)

func main() {
	cfg := config.Load()

	// -------------------------------------------------------------------------
	// RabbitMQ
	// -------------------------------------------------------------------------
	rmq, err := queue.NewRabbitMQ(&queue.Config{
		URL:       cfg.RabbitMQURL,
		QueueName: cfg.RabbitMQQueue,
	})
	if err != nil {
		log.Fatal("RabbitMQ connect failed:", err)
	}

	// -------------------------------------------------------------------------
	// Worker Registry
	// -------------------------------------------------------------------------
	reg := worker.NewRegistry()

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		for range ticker.C {
			reg.CleanupExpired()
		}
	}()

	// -------------------------------------------------------------------------
	// gRPC server
	// -------------------------------------------------------------------------
	lis, err := net.Listen("tcp", cfg.GRPCAddr())
	if err != nil {
		log.Fatal("gRPC listen failed:", err)
	}

	grpcServer := grpc.NewServer()
	workerpb.RegisterWorkerServiceServer(grpcServer, worker.NewGRPCServer(reg))

	go func() {
		log.Printf("gRPC server listening on %s", cfg.GRPCAddr())
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatal("gRPC server error:", err)
		}
	}()

	// -------------------------------------------------------------------------
	// HTTP server
	// -------------------------------------------------------------------------
	mux := http.NewServeMux()
	mux.HandleFunc("/workers", handleGetWorkers(reg))
	mux.HandleFunc("/jobs", handlePostJob(reg, rmq))

	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr(),
		Handler: mux,
	}

	go func() {
		log.Printf("API listening on %s", cfg.HTTPAddr())
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("HTTP server error:", err)
		}
	}()

	// -------------------------------------------------------------------------
	// Graceful shutdown
	// -------------------------------------------------------------------------
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down — draining in-flight requests...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Println("HTTP server forced to shut down:", err)
	}

	grpcServer.GracefulStop()
	rmq.Close()

	log.Println("API server stopped cleanly")
}

// GET /workers
func handleGetWorkers(reg *worker.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		snapshot := reg.List()

		type workerJSON struct {
			WorkerID string `json:"worker_id"`
			LastSeen int64  `json:"last_seen_unix"`
			Load     int32  `json:"load"`
			Status   string `json:"status"`
		}

		list := make([]workerJSON, 0, len(snapshot))
		for id, info := range snapshot {
			status := "idle"
			if info.Load > 0 {
				status = "busy"
			}
			list = append(list, workerJSON{
				WorkerID: id,
				LastSeen: info.LastSeen.Unix(),
				Load:     info.Load,
				Status:   status,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"count":   len(list),
			"workers": list,
		})
	}
}

// POST /jobs
func handlePostJob(reg *worker.Registry, rmq *queue.RabbitMQ) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Type    string      `json:"type"`
			Payload interface{} `json:"payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Type == "" {
			http.Error(w, "field 'type' is required", http.StatusBadRequest)
			return
		}

		job := jobs.Job{
			ID:      uuid.NewString(),
			Type:    req.Type,
			Payload: req.Payload,
		}

		workerID := reg.PickWorker()

		if workerID == "" {
			log.Printf("no alive workers — queuing job %s", job.ID)
			enqueueToRabbitMQ(w, job, rmq, "no alive workers, job queued")
			return
		}

		err := reg.DispatchJob(workerID, job.ID, job.Type, job.Payload)
		if err != nil {
			log.Printf("dispatch to worker %s failed: %v — queuing job %s", workerID, err, job.ID)
			enqueueToRabbitMQ(w, job, rmq, "worker unreachable, job queued")
			return
		}

		log.Printf("job %s dispatched directly to worker %s", job.ID, workerID)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"job":       job,
			"routed":    "direct",
			"worker_id": workerID,
		})
	}
}

func enqueueToRabbitMQ(w http.ResponseWriter, job jobs.Job, rmq *queue.RabbitMQ, note string) {
	data, err := json.Marshal(job)
	if err != nil {
		http.Error(w, "failed to encode job: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := rmq.Publish(data); err != nil {
		http.Error(w, "failed to enqueue job: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"job":    job,
		"routed": "queue",
		"note":   note,
	})
}
