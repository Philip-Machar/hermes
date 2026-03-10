package worker

import (
	"context"
	"log"
	"strings"

	"jobqueue/proto/workerpb"
)

// GRPCServer handles calls from workers: Register, Heartbeat, ListWorkers.
// It runs on the API server at :50051.
type GRPCServer struct {
	registry *Registry
	workerpb.UnimplementedWorkerServiceServer
}

func NewGRPCServer(reg *Registry) *GRPCServer {
	return &GRPCServer{registry: reg}
}

// Register is called by a worker on startup.
// worker_id format: "<uuid>@<host:port>"
// The host:port is the worker's own WorkerDispatchService address.
func (s *GRPCServer) Register(ctx context.Context, req *workerpb.RegisterRequest) (*workerpb.RegisterResponse, error) {
	id, address := splitWorkerID(req.WorkerId)
	log.Printf("worker registered: id=%s address=%s", id, address)
	s.registry.Register(id, address)
	return &workerpb.RegisterResponse{Status: "ok"}, nil
}

func (s *GRPCServer) Heartbeat(ctx context.Context, req *workerpb.HeartbeatRequest) (*workerpb.HeartbeatResponse, error) {
	s.registry.UpdateLoad(req.WorkerId, req.Load)
	return &workerpb.HeartbeatResponse{Status: "alive"}, nil
}

func (s *GRPCServer) ListWorkers(ctx context.Context, _ *workerpb.Empty) (*workerpb.WorkerList, error) {
	snapshot := s.registry.List()

	resp := &workerpb.WorkerList{}
	for id, info := range snapshot {
		resp.Workers = append(resp.Workers, &workerpb.Worker{
			WorkerId:     id,
			LastSeenUnix: info.LastSeen.Unix(),
			Load:         info.Load,
		})
	}
	return resp, nil
}

// splitWorkerID splits "uuid@host:port" into ("uuid", "host:port").
// If there is no "@", address is empty.
func splitWorkerID(raw string) (id, address string) {
	parts := strings.SplitN(raw, "@", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return raw, ""
}
