package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"sync"
	"time"
)

const workerTTL = time.Second * 15

type workerInfo struct {
	LastSeen time.Time
	Load     int32
	Address  string
}

type Registry struct {
	mu      sync.Mutex
	workers map[string]*workerInfo
}

func NewRegistry() *Registry {
	return &Registry{
		workers: make(map[string]*workerInfo),
	}
}

func (r *Registry) Register(id, address string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workers[id] = &workerInfo{
		LastSeen: time.Now(),
		Load:     0,
		Address:  address,
	}
}

func (r *Registry) UpdateLoad(id string, load int32) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if w, ok := r.workers[id]; ok {
		w.Load = load
		w.LastSeen = time.Now()
	}
}

func (r *Registry) List() map[string]workerInfo {
	r.mu.Lock()
	defer r.mu.Unlock()

	snapshot := make(map[string]workerInfo)
	for id, info := range r.workers {
		snapshot[id] = *info
	}
	return snapshot
}

// PickWorker returns the ID of the worker with the lowest load.
// If multiple workers share the lowest load, the first one found wins.
// Returns "" if no workers are registered.
func (r *Registry) PickWorker() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var bestID string
	var bestLoad int32 = math.MaxInt32

	for id, info := range r.workers {
		if info.Load < bestLoad {
			bestLoad = info.Load
			bestID = id
		}
	}

	return bestID
}

// DispatchJob sends a job directly to a worker over HTTP.
func (r *Registry) DispatchJob(workerID, jobID, jobType string, payload interface{}) error {
	r.mu.Lock()
	info, ok := r.workers[workerID]
	if !ok {
		r.mu.Unlock()
		return errors.New("worker not found: " + workerID)
	}
	addr := info.Address
	r.mu.Unlock()

	if addr == "" {
		return errors.New("worker has no dispatch address: " + workerID)
	}

	body, err := json.Marshal(map[string]interface{}{
		"job_id":   jobID,
		"job_type": jobType,
		"payload":  payload,
	})
	if err != nil {
		return fmt.Errorf("failed to encode dispatch request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/dispatch", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("dispatch request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("worker returned status %d", resp.StatusCode)
	}

	return nil
}

func (r *Registry) CleanupExpired() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for id, info := range r.workers {
		if now.Sub(info.LastSeen) > workerTTL {
			log.Printf("Worker %s expired — removing", id)
			delete(r.workers, id)
		}
	}
}
