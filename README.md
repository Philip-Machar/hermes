# Hermes

A production-grade distributed job processing system built in Go — featuring a gRPC control plane, intelligent load-balanced HTTP dispatch, RabbitMQ-backed durability, graceful shutdown, a real-time web dashboard, and a fully containerised deployment.

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![RabbitMQ](https://img.shields.io/badge/RabbitMQ-3.13-FF6600?style=flat&logo=rabbitmq)](https://www.rabbitmq.com/)
[![gRPC](https://img.shields.io/badge/gRPC-1.78-244c5a?style=flat&logo=grpc)](https://grpc.io/)
[![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?style=flat&logo=docker)](https://docs.docker.com/compose/)
[![CI](https://github.com/yourname/hermes/actions/workflows/ci.yml/badge.svg)](https://github.com/yourname/hermes/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

---

## Overview

Hermes is built around a **control plane / data plane separation** — the same architectural principle used by systems like Kubernetes and Envoy. Workers self-register via gRPC, continuously heartbeat their load status, and receive jobs either through direct HTTP dispatch (the fast path) or via a durable RabbitMQ queue (the reliable fallback). The entire system starts with a single `docker-compose up`.

**What makes it interesting:**

- No external coordination service. The registry is in-memory with TTL-based expiry, kept consistent by the heartbeat protocol.
- Jobs are never dropped. If direct dispatch fails, they fall back to RabbitMQ automatically — no retry logic needed at the client.
- Workers report real load metrics. The router picks the least-loaded worker, not just any alive one.
- Real job handlers are registered at startup. Adding a new job type is a single `Register` call — no switch statements, no conditionals.
- The system shuts down cleanly. Both API and workers drain in-flight work before exiting.
- A real-time dashboard surfaces worker fleet status and job activity live at `GET /`.

---

## Screenshots

> _Real-time worker and job monitoring dashboard_

![Dashboard](docs/screenshots/dashboard.png)

---

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                          CLIENT                              │
│        curl / HTTP / Dashboard / Any HTTP client             │
└────────────────────────────┬─────────────────────────────────┘
                             │  HTTP
                             ▼
┌──────────────────────────────────────────────────────────────┐
│                     API SERVER  :8081                        │
│                                                              │
│   GET  /           ──►  Dashboard (embedded static UI)       │
│   POST /jobs       ──►  Registry.PickWorker()                │
│                          │                                   │
│                   least-loaded worker?                       │
│                   ┌──────┴──────┐                            │
│                  YES            NO                           │
│                   │              │                           │
│                   ▼              ▼                           │
│           HTTP Dispatch     RabbitMQ Queue                   │
│           (fast path)       (reliable fallback)              │
│                                                              │
│   GET /workers  ──►  Registry.List() snapshot                │
│   GET /events   ──►  Event ring (last 100 job events)        │
│                                                              │
│   gRPC :50051  ◄──  Workers (Register / Heartbeat)           │
└────────┬─────────────────────────┬───────────────────────────┘
         │ gRPC                    │ HTTP
         │ Register / Heartbeat    │ POST /dispatch
         ▼                        ▼
┌──────────────────┐     ┌──────────────────┐
│  WORKER NODE 1   │     │  WORKER NODE 2   │
│  :9001           │     │  :9002           │
│                  │     │                  │
│  Heartbeat 5s    │     │  Heartbeat 5s    │
│  RabbitMQ consumer     │  RabbitMQ consumer
└────────┬─────────┘     └────────┬─────────┘
         └──────────┬─────────────┘
                    │ AMQP
                    ▼
       ┌────────────────────────┐
       │       RabbitMQ         │
       │  jobs queue + DLQ      │
       │  :5672  │  UI :15672   │
       └────────────────────────┘
```

### Control plane — gRPC (Worker → API)

Workers initiate all control traffic. On startup each worker dials the API's gRPC server, registers its dispatch address, then enters a heartbeat loop every 5 seconds. The API maintains an in-memory registry and evicts any worker that hasn't heartbeated within 15 seconds. This means the API always has a consistent, live view of the fleet without polling.

### Data plane — HTTP (API → Worker)

When a job arrives the API scores every alive worker by current load and POSTs the job directly to the least-loaded one's `/dispatch` endpoint. No queue round-trip, no broker latency. If no workers are alive, or if dispatch fails for any reason, the job is published to RabbitMQ so it will be picked up as soon as capacity is available.

### Reliability layer — RabbitMQ

Every worker also consumes from the same RabbitMQ queue. Jobs that cannot be dispatched directly, or that fail during processing, are retried up to 3 times. After 3 failed attempts they are moved to a Dead Letter Queue for inspection — nothing is silently dropped.

---

## Key Design Decisions

**Why gRPC for the control plane and HTTP for dispatch?**
Control messages (register, heartbeat) are low-volume, structured, and benefit from the strong typing and streaming primitives of gRPC. Job dispatch is higher-volume, payload-heavy, and simpler to implement and debug over plain HTTP/JSON. Separating the two planes also means you can evolve each independently.

**Why in-memory registry instead of Redis?**
For a single-node deployment, an in-memory registry with TTL-based expiry is simpler, faster, and has no external dependency. The heartbeat protocol provides the same consistency guarantees as a distributed cache — if a worker stops heartbeating, it's evicted within one TTL window. Redis becomes the right call when you need the registry to survive API restarts or scale across multiple API nodes.

**Why least-loaded routing instead of round-robin?**
Workers report a live load counter with every heartbeat. Routing to the least-loaded worker is a 5-line change that meaningfully reduces tail latency under uneven workloads — and is a more accurate reflection of real capacity than a simple counter.

**Why a handler registry instead of a switch statement?**
Each job type is a registered `Handler` implementation. The worker looks up the handler by `job.Type` at runtime — adding a new job type means writing a struct and one `Register` call at startup, with no changes to the dispatch or retry logic.

---

## Features

| Feature | Detail |
|---|---|
| Direct dispatch | API pushes jobs straight to a worker over HTTP — no queue latency when workers are available |
| Intelligent routing | Least-loaded worker selection based on live heartbeat metrics |
| Automatic failover | Falls back to RabbitMQ transparently when no workers are alive or dispatch fails |
| Worker registry | In-memory registry with TTL-based eviction (15 s) |
| Heartbeat monitoring | Workers report load and status every 5 seconds |
| Job handler registry | Extensible handler pattern — add new job types with a single Register call |
| Retry logic | Failed queue jobs retry up to 3 times before DLQ |
| Dead Letter Queue | Failed jobs are captured for inspection, never silently dropped |
| Graceful shutdown | API and workers drain in-flight work on SIGINT / SIGTERM before exiting |
| Real-time dashboard | Live worker fleet and job activity log served at `GET /` |
| Event ring | Server-side ring buffer records the last 100 job events, surfaced via `GET /events` |
| Environment-driven config | No hardcoded credentials — everything configurable via environment variables |
| Single-command deployment | `docker-compose up --build` starts RabbitMQ, API, and worker with health-checked ordering |
| Multi-worker | Scale horizontally by starting additional worker instances — each self-registers |
| Integration tests | testcontainers-go spins up a real RabbitMQ container to test the full job flow end to end |
| CI pipeline | GitHub Actions runs unit and integration tests on every push and pull request |

---

## Tech Stack

| Component | Technology |
|---|---|
| Language | Go 1.24+ |
| Control plane | gRPC + Protocol Buffers |
| Message broker | RabbitMQ 3.13 (AMQP 0.9.1) |
| Job dispatch | HTTP/JSON |
| Worker coordination | In-memory registry with TTL |
| Dashboard | Embedded single-page HTML (Go embed.FS) |
| Infrastructure | Docker Compose (multi-stage builds) |
| Testing | testcontainers-go, Go testing package |
| CI | GitHub Actions |

---

## Project Structure

```
hermes/
├── .github/
│   └── workflows/
│       └── ci.yml                # CI — unit + integration tests on every push
├── cmd/
│   ├── api/
│   │   ├── main.go               # API server — HTTP :8081 + gRPC :50051
│   │   ├── handlers.go           # HTTP handlers — /jobs, /workers, /events
│   │   └── static/
│   │       └── index.html        # Real-time dashboard (embedded into binary)
│   └── worker/
│       ├── worker_main/
│       │   └── main.go           # Worker node — dispatch server + queue consumer
│       └── dlq_consumer/
│           └── main.go           # Dead-letter queue inspector
├── internal/
│   ├── config/
│   │   └── config.go             # Environment-driven configuration
│   ├── events/
│   │   └── ring.go               # Thread-safe ring buffer for job events
│   ├── jobs/
│   │   ├── job.go                # Job struct
│   │   ├── handler.go            # Handler interface + HandlerRegistry
│   │   └── handlers/
│   │       ├── email.go          # Email job handler
│   │       └── resize_image.go   # Resize-image job handler
│   ├── queue/
│   │   ├── config.go             # RabbitMQ topology config
│   │   ├── queue.go              # Queue interface
│   │   └── rabbitmq.go           # RabbitMQ implementation, DLQ setup
│   └── worker/
│       ├── registry.go           # In-memory registry with least-loaded routing
│       └── grpc_server.go        # gRPC handler — Register, Heartbeat, ListWorkers
├── internal/integration/
│   └── queue_test.go             # End-to-end integration tests (testcontainers-go)
├── proto/
│   ├── worker.proto              # Protobuf service definition
│   └── workerpb/
│       ├── worker.pb.go          # Generated message types
│       └── worker_grpc.pb.go     # Generated gRPC stubs
├── docker/
│   ├── Dockerfile.api            # Multi-stage build for API
│   └── Dockerfile.worker         # Multi-stage build for worker
├── docker-compose.yml
├── .env.example                  # Config template — copy to .env
├── go.mod
└── README.md
```

---

## Quick Start

### Option A — Docker (recommended)

```bash
git clone https://github.com/yourname/hermes
cd hermes
cp .env.example .env
docker-compose up --build
```

RabbitMQ starts first with a health check. The API waits for RabbitMQ to be ready. The worker waits for both. Everything comes up in the right order automatically.

Then open **http://localhost:8081** to view the live dashboard.

### Option B — Local development

**Prerequisites:** Go 1.24+, Docker (for RabbitMQ)

```bash
# 1. Start RabbitMQ only
docker-compose up -d rabbitmq

# 2. Start the API
go run ./cmd/api

# 3. Start a worker (separate terminal)
go run ./cmd/worker/worker_main/main.go

# 4. Scale horizontally — each worker self-registers on its own port
API_GRPC_ADDR=localhost:50051 WORKER_DISPATCH_PORT=9002 go run ./cmd/worker/worker_main/main.go
API_GRPC_ADDR=localhost:50051 WORKER_DISPATCH_PORT=9003 go run ./cmd/worker/worker_main/main.go
```

### Test it

```bash
# Open the dashboard
open http://localhost:8081

# Check the worker fleet
curl http://localhost:8081/workers

# Submit a job
curl -X POST http://localhost:8081/jobs \
  -H "Content-Type: application/json" \
  -d '{"type":"email","payload":{"to":"user@example.com","subject":"Hello"}}'

# Flood the system with 10 concurrent jobs
for i in {1..10}; do
  curl -s -X POST http://localhost:8081/jobs \
    -H "Content-Type: application/json" \
    -d "{\"type\":\"email\",\"payload\":{\"to\":\"user$i@example.com\",\"subject\":\"Test $i\"}}" &
done
```

**Direct dispatch response (worker alive):**
```json
{
  "job": {
    "id": "1d3de6cf-f005-490d-aeae-748c8c0949f9",
    "type": "email",
    "payload": { "to": "user@example.com", "subject": "Hello" },
    "attempts": 0
  },
  "routed": "direct",
  "worker_id": "8a4605e1-cd04-42d3-8df7-2a3401b47546"
}
```

**Fallback response (no workers available):**
```json
{
  "job": { ... },
  "routed": "queue",
  "note": "no alive workers, job queued"
}
```

---

## API Reference

### `GET /`

Serves the real-time dashboard — live worker fleet status, load metrics, and a scrolling activity log of all job events. Also provides a job submission modal.

---

### `POST /jobs`

Submit a job for processing.

**Request:**
```json
{
  "type": "email",
  "payload": { "to": "user@example.com" }
}
```

**Response `202 Accepted`:**
```json
{
  "job": { "id": "...", "type": "email", "payload": {}, "attempts": 0 },
  "routed": "direct",
  "worker_id": "8a4605e1-..."
}
```

`routed` is either `"direct"` (dispatched to a live worker) or `"queue"` (published to RabbitMQ).

**Supported job types:** `email`, `resize-image`

---

### `GET /workers`

Live snapshot of the worker fleet.

**Response:**
```json
{
  "count": 2,
  "workers": [
    {
      "worker_id": "8a4605e1-...",
      "last_seen_unix": 1741614789,
      "load": 0,
      "status": "idle"
    },
    {
      "worker_id": "37a2134a-...",
      "last_seen_unix": 1741614791,
      "load": 2,
      "status": "busy"
    }
  ]
}
```

---

### `GET /events`

The last 100 job events recorded by the API, newest first. Used by the dashboard to populate the activity log.

**Response:**
```json
{
  "count": 3,
  "events": [
    {
      "time": "2025-01-01T12:00:02Z",
      "job_id": "1d3de6cf-...",
      "job_type": "email",
      "kind": "direct",
      "worker_id": "8a4605e1-...",
      "message": "dispatched direct → 8a4605e1"
    },
    {
      "time": "2025-01-01T12:00:01Z",
      "job_id": "2e4df7ag-...",
      "job_type": "resize-image",
      "kind": "queued",
      "message": "published to queue — no alive workers"
    }
  ]
}
```

`kind` is one of `direct`, `queued`, or `error`.

---

## Adding a New Job Type

1. Create `internal/jobs/handlers/your_type.go` implementing the `jobs.Handler` interface:

```go
type YourHandler struct{}

func (h *YourHandler) Handle(ctx context.Context, job jobs.Job) error {
    // your logic here
    return nil
}
```

2. Register it in `cmd/worker/worker_main/main.go`:

```go
func init() {
    handlerRegistry = jobs.NewHandlerRegistry()
    handlerRegistry.Register("email", &handlers.EmailHandler{})
    handlerRegistry.Register("resize-image", &handlers.ResizeImageHandler{})
    handlerRegistry.Register("your-type", &handlers.YourHandler{})  // add this
}
```

That's it. No changes to routing, retry, or DLQ logic.

---

## Testing

```bash
# Unit tests
go test ./...

# Integration tests (requires Docker)
go test -tags integration -timeout=120s ./internal/integration/...
```

Integration tests use testcontainers-go to spin up a real RabbitMQ container and verify the full `POST /jobs → queue → consumer → ack` flow end to end, including the retry loop and DLQ path.

---

## Configuration

All configuration is via environment variables. Copy `.env.example` to `.env` and fill in values.

| Variable | Default | Description |
|---|---|---|
| `RABBITMQ_URL` | `amqp://guest:guest@localhost:5672/` | Full AMQP connection string |
| `RABBITMQ_USER` | `guest` | RabbitMQ username (used by Docker Compose) |
| `RABBITMQ_PASS` | `guest` | RabbitMQ password (used by Docker Compose) |
| `RABBITMQ_QUEUE` | `jobs` | Main job queue name |
| `RABBITMQ_DLQ` | `jobs.dlq` | Dead letter queue name |
| `API_HTTP_PORT` | `8081` | API HTTP port |
| `API_GRPC_PORT` | `50051` | gRPC control plane port |
| `API_GRPC_ADDR` | `localhost:50051` | Address workers dial to register |
| `WORKER_HOST` | `localhost` | Hostname workers advertise for dispatch (set to `worker` in Docker) |
| `WORKER_DISPATCH_PORT` | `9001` | Port the worker's HTTP dispatch server listens on |

---

## How Job Routing Works

```
POST /jobs
    │
    ▼
Registry.PickWorker()  ←  scores all alive workers by current load
    │
    ├── worker found
    │       │
    │       ▼
    │   HTTP POST → worker /dispatch
    │       │
    │       ├── 200 OK  →  202 { routed: "direct", worker_id: "..." }
    │       │               event kind: "direct" pushed to ring
    │       │
    │       └── error   →  publish to RabbitMQ
    │                          │
    │                      202 { routed: "queue" }
    │                          event kind: "error" pushed to ring
    │
    └── no workers alive  →  publish to RabbitMQ
                                   │
                               202 { routed: "queue" }
                                   event kind: "queued" pushed to ring
```

---

## Worker Lifecycle

```
startup
   │
   ├─ connect to RabbitMQ (retry loop, up to 20 attempts)
   ├─ start HTTP /dispatch server
   ├─ dial API gRPC :50051
   ├─ Register("<uuid>@<host>:<port>")
   │
   ├─ heartbeat goroutine (every 5 s) ─────────────────────────┐
   │       sends: worker_id, load counter                      │
   │                                                           │
   ├─ RabbitMQ consumer goroutine                              │
   │       │                                                   │
   │       ├─ message received                                 │
   │       │       │                                           │
   │       │       ▼                                           │
   │       │   HandlerRegistry.Dispatch(ctx, job)              │
   │       │       │                                           │
   │       │       ├─ success → Ack                            │
   │       │       │                                           │
   │       │       └─ failure → increment attempts             │
   │       │               │                                   │
   │       │               ├─ attempts < 3 → re-publish        │
   │       │               │                                   │
   │       │               └─ attempts == 3 → Nack → DLQ       │
   │                                                           │
   └─ SIGINT / SIGTERM received                                │
           │                                                   │
           ├─ HTTP server: graceful shutdown (10 s timeout) ◄──┘
           ├─ RabbitMQ connection: close
           └─ gRPC connection: close
```

---

## Graceful Shutdown

Both the API and worker handle `SIGINT` and `SIGTERM`. On receiving a signal:

- The **API** stops accepting new HTTP requests, stops the gRPC server, and closes the RabbitMQ connection.
- Each **worker** shuts down its dispatch HTTP server with a 10-second timeout, closes its RabbitMQ consumer, and closes its gRPC connection to the API.

A rolling deployment or `docker-compose down` will not drop in-flight jobs.

---

## Running the DLQ Consumer

To inspect jobs that have exhausted all retries:

```bash
go run ./cmd/worker/dlq_consumer/main.go
```

---

## RabbitMQ Management UI

[http://localhost:15672](http://localhost:15672) — username and password set in `.env`

Inspect queue depths, message rates, and dead-lettered jobs here.

---

## Roadmap

- [x] Web dashboard — real-time worker and job monitoring UI
- [x] Integration tests — testcontainers-go end-to-end test suite
- [x] CI pipeline — GitHub Actions on every push and pull request
- [ ] Landing page
- [ ] Prometheus metrics endpoint (`/metrics`)
- [ ] Job status tracking — pending / running / complete / failed
- [ ] Priority queues
- [ ] Distributed registry (Redis) for multi-API-node deployments

---

## Author

Built by **Philip Machar**

---

## License

MIT