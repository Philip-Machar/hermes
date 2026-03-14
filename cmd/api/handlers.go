package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"

	"jobqueue/internal/events"
	"jobqueue/internal/jobs"
	"jobqueue/internal/queue"
	"jobqueue/internal/worker"
)

// shortID returns the first 8 characters of id, or id itself if shorter.
func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// jobsHandler handles POST /jobs.
func jobsHandler(registry *worker.Registry, rmq queue.Queue, ring *events.Ring) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		workerID := registry.PickWorker()
		if workerID != "" {
			if err := registry.DispatchJob(workerID, job.ID, job.Type, job.Payload); err == nil {
				log.Printf("job %s dispatched directly to worker %s", job.ID, workerID)
				ring.Push(events.Event{
					JobID:    job.ID,
					JobType:  job.Type,
					Kind:     events.KindDirect,
					WorkerID: workerID,
					Message:  "dispatched direct → " + shortID(workerID),
				})
				json.NewEncoder(w).Encode(map[string]interface{}{
					"job":       job,
					"routed":    "direct",
					"worker_id": workerID,
				})
				return
			}
			log.Printf("direct dispatch to worker %s failed, falling back to queue", workerID)
			ring.Push(events.Event{
				JobID:   job.ID,
				JobType: job.Type,
				Kind:    events.KindError,
				Message: "direct dispatch failed, falling back to queue",
			})
		}

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
		ring.Push(events.Event{
			JobID:   job.ID,
			JobType: job.Type,
			Kind:    events.KindQueued,
			Message: "published to queue — no alive workers",
		})
		json.NewEncoder(w).Encode(map[string]interface{}{
			"job":    job,
			"routed": "queue",
			"note":   "no alive workers, job queued",
		})
	}
}

// workersHandler handles GET /workers.
func workersHandler(registry *worker.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
	}
}

// eventsHandler handles GET /events — returns the recent activity ring as JSON.
func eventsHandler(ring *events.Ring) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		snapshot := ring.Snapshot()

		// Return newest-first so the dashboard can prepend without reversing.
		reversed := make([]events.Event, len(snapshot))
		for i, e := range snapshot {
			reversed[len(snapshot)-1-i] = e
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"count":  len(reversed),
			"events": reversed,
		})
	}
}
