# JobQueue - Distributed Job Processing System

A scalable, fault-tolerant distributed job queue system built with Go, RabbitMQ, and gRPC. This system allows you to submit jobs from a central API and have them processed by a fleet of worker nodes with automatic retry logic and dead-letter queue handling.

## Architecture

```
┌─────────────┐                     ┌──────────────┐
│   Client    │─────(HTTP)────────> │  API Server  │
│             │                     │   :8080      │
└─────────────┘                     └──────────────┘
                                           │
                              (JSON Jobs)  │
                                           ▼
                                    ┌────────────┐
                                    │ RabbitMQ   │
                                    │ :5672      │
                                    └────────────┘
                                       ▲  │   ▲
                              (Consume)│  │  │ (DLQ)
                                       │  ▼  │
                        ┌────────────────-┴──────────────┐
                        │                                │
                   ┌────────┐                      ┌──────────┐
                   │ Worker │ (gRPC Heartbeat)     │ DLQ Jobs │
                   │ :50051 │◄────────────────────-│          │
                   │ Node 1 │                      └──────────┘
                   └────────┘
                        │
                        ├─ gRPC Heartbeat (every 5s)
                        │
                   ┌────────┐
                   │ Worker │
                   │ Node 2 │
                   └────────┘
                        │
                        ├─ Processes Jobs
                        │
                        └─ Retries on Failure (3 attempts)
```

## Features

### Core Functionality
- **Job Submission**: Submit jobs via HTTP REST API with arbitrary payload
- **Distributed Processing**: Multiple worker nodes process jobs concurrently
- **Automatic Retries**: Failed jobs automatically retry (up to 3 attempts) before going to DLQ
- **Dead Letter Queue**: Failed jobs after max retries are moved to a DLQ for analysis
- **Worker Health Tracking**: Workers register with the API and send heartbeats every 5 seconds

### Reliability
- **Persistent Queues**: All jobs persisted in RabbitMQ
- **Worker Registry**: In-memory registry tracks all active workers with automatic cleanup
- **Load Awareness**: Workers report their load status (idle/busy) to enable load balancing
- **gRPC Communication**: Efficient binary protocol for worker-to-API communication

## Tech Stack

- **Language**: Go 1.21+
- **Message Queue**: RabbitMQ 3.x (AMQP 0.9.1)
- **RPC Framework**: gRPC with Protocol Buffers
- **Containerization**: Docker & Docker Compose

## Quick Start

### Prerequisites

- Docker & Docker Compose
- Go 1.21+

### 1. Start RabbitMQ

```bash
docker-compose up -d
```

Verify RabbitMQ is running:
```bash
docker-compose ps
```

### 2. Start the API Server

In a new terminal:
```bash
go run ./cmd/api/main.go
```

You should see:
```
gRPC server listening on :50051...
Job API listening on :8080...
```

### 3. Start Worker Node(s)

In another terminal:
```bash
go run ./cmd/worker/worker_main/main.go
```

You should see:
```
Worker registered with ID: <uuid>
Worker started and consuming jobs
heartbeat sent (load=0)
heartbeat sent (load=0)
...
```

### 4. Submit a Job

In yet another terminal:
```bash
curl -X POST http://localhost:8080/jobs \
  -H "Content-Type: application/json" \
  -d '{"type":"email","payload":{"to":"user@example.com","subject":"Hello"}}'
```

You'll get a response like:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "type": "email",
  "payload": {"to": "user@example.com", "subject": "Hello"},
  "attempts": 0
}
```

The worker will process the job and log:
```
processing job 550e8400-e29b-41d4-a716-446655440000 (type=email, attempt=0)
job 550e8400-e29b-41d4-a716-446655440000 succeeded
```

## API Endpoints

### Submit a Job
**POST** `/jobs`

Request body:
```json
{
  "type": "string",           // Job type identifier
  "payload": {}              // Arbitrary job payload
}
```

Response (202 Accepted):
```json
{
  "id": "uuid",              // Unique job ID
  "type": "string",
  "payload": {},
  "attempts": 0              // Attempt count
}
```

## gRPC Service

The API exposes a `WorkerService` on `:50051` for worker coordination.

### WorkerService API

#### Register
Registers a new worker with the control plane.
- **Request**: `RegisterRequest{worker_id: string}`
- **Response**: `RegisterResponse{status: string}`

#### Heartbeat
Sends periodic health checks from worker to API.
- **Request**: `HeartbeatRequest{worker_id: string, load: int32}`
- **Response**: `HeartbeatResponse{status: string}`
- **Interval**: Every 5 seconds per worker
- **Load Values**: 0 = idle, 1 = busy processing

#### ListWorkers
Retrieves list of all active workers.
- **Request**: `Empty{}`
- **Response**: `WorkerList{workers: map[string]Timestamp]}`

## Job Processing Flow

```
1. Client submits job via HTTP POST /jobs
                    ↓
2. API Server validates & publishes to RabbitMQ "jobs" queue
                    ↓
3. Worker consumes message from queue
                    ↓
4. Job processing:
   - Unmarshal JSON payload
   - Simulate processing (demo: always succeeds on 3rd attempt)
   - If failed (attempts < 3):
     * Re-marshal job with incremented attempts
     * Nack original message
     * Re-publish to jobs queue
   - If succeeded:
     * Ack message (remove from queue)
```

## Retry Logic

Jobs are retried automatically up to **3 total attempts**:

- **Attempt 0**: Initial processing → Fails → Re-queue
- **Attempt 1**: Second try → Fails → Re-queue  
- **Attempt 2**: Third try → Succeeds (or goes to DLQ)

Failed jobs after 3 attempts move to the **Dead Letter Queue (DLQ)** for manual inspection.

## Worker Health Management

### Registration
When a worker starts, it:
1. Connects to API gRPC server
2. Calls `Register(worker_id)`
3. Enters heartbeat loop

### Heartbeat Cycle
Every 5 seconds, workers send:
- Current `worker_id`
- Current `load` status (0=idle, 1=busy)

### Cleanup
The API periodically (every 5 seconds) removes workers that haven't sent a heartbeat for **15 seconds**.

## Project Structure

```
jobqueue/
├── cmd/
│   ├── api/
│   │   └── main.go              # API server (HTTP + gRPC)
│   └── worker/
│       ├── worker_main/
│       │   └── main.go          # Worker node
│       └── dlq_consumer/
│           └── main.go          # Dead-letter queue consumer (optional)
├── internal/
│   ├── jobs/
│   │   └── job.go               # Job data structure
│   ├── queue/
│   │   ├── config.go            # RabbitMQ config
│   │   ├── queue.go             # Queue interface
│   │   └── rabbitmq.go          # RabbitMQ implementation
│   └── worker/
│       ├── registry.go          # Worker registry (in-memory)
│       └── grpc_server.go       # gRPC service handler
├── proto/
│   ├── worker.proto             # Protocol Buffer definitions
│   └── workerpb/
│       ├── worker.pb.go         # Generated message code
│       └── worker_grpc.pb.go    # Generated gRPC code
├── docker-compose.yml           # Local development setup
├── go.mod & go.sum              # Go dependencies
└── README.md                    # This file
```

## Configuration

### RabbitMQ Connection
Edit the URL in `cmd/api/main.go` and `cmd/worker/worker_main/main.go`:
```go
cfg := queue.Config{
    URL:       "amqp://guest:guest@localhost:5672/",
    QueueName: "jobs",
}
```

### Worker Heartbeat Interval
Edit `cmd/worker/worker_main/main.go`:
```go
ticker := time.NewTicker(5 * time.Second)  // Change to desired interval
```

### Worker TTL (Time To Live)
Edit `internal/worker/registry.go`:
```go
const workerTTL = time.Second * 15  // Workers removed after 15s of no heartbeat
```

## Development

### Generate Protocol Buffers

If you modify `proto/worker.proto`, regenerate the Go code:

```bash
protoc --go_out=. --go-grpc_out=. proto/worker.proto
```

### Run Tests

```bash
go test ./...
```

### View RabbitMQ Management UI

Open http://localhost:15672 in your browser
- Username: `guest`
- Password: `guest`

You can inspect queues, messages, and dead-letter exchanges here.

## Troubleshooting

### Worker hangs on startup
**Symptom**: Worker prints registration message but no heartbeats.

**Solution**: Ensure API server is running on `:50051` and RabbitMQ is accessible.

### Jobs not processing
**Symptom**: Jobs published but workers don't consume them.

**Solution**: 
1. Check RabbitMQ is running: `docker-compose ps`
2. Verify queue name matches in both API and Worker configs
3. Check worker logs for connection errors

### High memory usage
**Symptom**: Registry grows unbounded.

**Solution**: The cleanup goroutine removes inactive workers every 5 seconds. Ensure no worker is permanently stuck.

## Performance Considerations

- **Concurrency**: Set QoS in worker to process multiple jobs in parallel (currently set to 1)
- **Scaling**: Add more worker nodes by running additional instances
- **Monitoring**: Expose worker registry via HTTP endpoint for metrics/dashboards
- **Persistence**: Currently uses in-memory registry; consider Redis for distributed deployments

## Future Enhancements

- [ ] Distributed worker registry (Redis/etcd)
- [ ] Job progress tracking and status updates
- [ ] Priority queues for job types
- [ ] Worker autoscaling based on queue depth
- [ ] Comprehensive metrics and observability (Prometheus)
- [ ] Job scheduling and cron support
- [ ] Web dashboard for job monitoring

## License

MIT

## Author

Built by Philip Machar
