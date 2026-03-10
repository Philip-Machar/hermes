package worker

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"jobqueue/proto/workerpb"
)

const workerTTL = time.Second * 15

type workerInfo struct {
	LastSeen time.Time
	Load     int32
	Address  string // gRPC dispatch address, e.g. "localhost:50052"
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

// Register adds a worker with its dispatch address.
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

// List returns a safe snapshot of all current workers.
func (r *Registry) List() map[string]workerInfo {
	r.mu.Lock()
	defer r.mu.Unlock()

	snapshot := make(map[string]workerInfo)
	for id, info := range r.workers {
		snapshot[id] = *info
	}
	return snapshot
}

// PickWorker returns the ID of the first alive worker, or "" if none.
// Simple strategy: first entry in the map. No load balancing needed yet.
func (r *Registry) PickWorker() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id := range r.workers {
		return id
	}
	return ""
}

// GetWorkerConn dials the dispatch gRPC address of the given worker.
// Returns the client and the underlying connection.
// Caller must call conn.Close() when done.
func (r *Registry) GetWorkerConn(id string) (workerpb.WorkerDispatchServiceClient, *grpc.ClientConn, error) {
	r.mu.Lock()
	info, ok := r.workers[id]
	if !ok {
		r.mu.Unlock()
		return nil, nil, errors.New("worker not found: " + id)
	}
	addr := info.Address
	r.mu.Unlock()

	if addr == "" {
		return nil, nil, errors.New("worker has no dispatch address registered: " + id)
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}

	return workerpb.NewWorkerDispatchServiceClient(conn), conn, nil
}

// DispatchContext returns a context with a 10-second timeout for dispatch calls.
func DispatchContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
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
