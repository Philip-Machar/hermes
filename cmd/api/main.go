package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"jobqueue/internal/config"
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
	mux.HandleFunc("/jobs", jobsHandler(registry, rmq))
	mux.HandleFunc("/workers", workersHandler(registry))

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
