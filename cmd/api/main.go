package main

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"time"

	"jobqueue/internal/jobs"
	"jobqueue/internal/queue"
	"jobqueue/internal/worker"
	"jobqueue/proto/workerpb"

	"github.com/google/uuid"
	"google.golang.org/grpc"
)

func main() {

	// -------------------------------------------------------------------------
	// RabbitMQ — fallback when no alive workers are available
	// -------------------------------------------------------------------------
	cfg := queue.Config{
		URL:       "amqp://guest:guest@localhost:5672/",
		QueueName: "jobs",
	}

	rmq, err := queue.NewRabbitMQ(&cfg)
	if err != nil {
		log.Fatal("RabbitMQ connect failed:", err)
	}
	defer rmq.Close()

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
	// gRPC server — workers connect here to Register and Heartbeat
	// -------------------------------------------------------------------------
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatal("gRPC listen failed:", err)
	}

	grpcServer := grpc.NewServer()
	workerpb.RegisterWorkerServiceServer(grpcServer, worker.NewGRPCServer(reg))

	go func() {
		log.Println("gRPC server listening on :50051...")
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatal("gRPC server error:", err)
		}
	}()

	// -------------------------------------------------------------------------
	// HTTP routes
	// -------------------------------------------------------------------------
	mux := http.NewServeMux()
	mux.HandleFunc("/workers", handleGetWorkers(reg))
	mux.HandleFunc("/jobs", handlePostJob(reg, rmq))

	log.Println("API listening on :8081...")
	log.Fatal(http.ListenAndServe(":8081", mux))
}

// GET /workers — returns all alive workers as JSON
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

// POST /jobs — dispatches directly to a worker over HTTP, falls back to RabbitMQ
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

		// No alive workers — fall back to RabbitMQ
		if workerID == "" {
			log.Printf("no alive workers — queuing job %s", job.ID)
			enqueueToRabbitMQ(w, job, rmq, "no alive workers, job queued")
			return
		}

		// Dispatch directly to the worker over HTTP
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
