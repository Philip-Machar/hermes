# Hermes

A production-grade distributed job processing system built in Go — featuring a gRPC control plane, intelligent load-balanced HTTP dispatch, RabbitMQ-backed durability, graceful shutdown, and a fully containerised deployment.

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![RabbitMQ](https://img.shields.io/badge/RabbitMQ-3.13-FF6600?style=flat&logo=rabbitmq)](https://www.rabbitmq.com/)
[![gRPC](https://img.shields.io/badge/gRPC-1.78-244c5a?style=flat&logo=grpc)](https://grpc.io/)
[![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?style=flat&logo=docker)](https://docs.docker.com/compose/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

---

## Overview

Hermes is built around a **control plane / data plane separation** — the same architectural principle used by systems like Kubernetes and Envoy. Workers self-register via gRPC, continuously heartbeat their load status, and receive jobs either through direct HTTP dispatch (the fast path) or via a durable RabbitMQ queue (the reliable fallback). The entire system starts with a single `docker-compose up`.

**What makes it interesting:**

- No external coordination service. The registry is in-memory with TTL-based expiry, kept consistent by the heartbeat protocol.
- Jobs are never dropped. If direct dispatch fails, they fall back to RabbitMQ automatically — no retry logic needed at the client.
- Workers report real load metrics. The router picks the least-loaded worker, not just any alive one.
- The system shuts down cleanly. Both API and workers drain in-flight work before exiting.

---

## Screenshots

> _Real-time worker and job monitoring dashboard_

<!-- Add after dashboard is built -->
![Dashboard](docs/screenshots/dashboard.png)

> _Landing page_

<!-- Add after landing page is built -->
![Landing](docs/screenshots/landing.png)

---

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│                          CLIENT                              │
│               curl / HTTP / Any HTTP client                  │
└────────────────────────────┬─────────────────────────────────┘
                             │  HTTP
                             ▼
┌──────────────────────────────────────────────────────────────┐
│                     API SERVER  :8081                        │
│                                                              │
│   POST /jobs  ──►  Registry.PickWorker()                     │
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

Every worker also consumes from the same RabbitMQ queue. Jobs that cannot be dispatched directly, or that fail during processing, are retried up to 3 times with exponential nack. After 3 failed attempts they are moved to a Dead Letter Queue for inspection — nothing is silently dropped.

---

## Key Design Decisions

**Why gRPC for the control plane and HTTP for dispatch?**
Control messages (register, heartbeat) are low-volume, structured, and benefit from the strong typing and streaming primitives of gRPC. Job dispatch is higher-volume, payload-heavy, and simpler to implement and debug over plain HTTP/JSON. Separating the two planes also means you can evolve each independently.

**Why in-memory registry instead of Redis?**
For a single-node deployment, an in-memory registry with TTL-based expiry is simpler, faster, and has no external dependency. The heartbeat protocol provides the same consistency guarantees as a distributed cache — if a worker stops heartbeating, it's evicted within one TTL window. Redis becomes the right call when you need the registry to survive API restarts or scale across multiple API nodes.

**Why least-loaded routing instead of round-robin?**
Workers report a live load counter with every heartbeat. Routing to the least-loaded worker is a 5-line change that meaningfully reduces tail latency under uneven workloads — and is a more accurate reflection of real capacity than a simple counter.

---

## Features

| Feature | Detail |
|---|---|
| Direct dispatch | API pushes jobs straight to a worker over HTTP — no queue latency when workers are available |
| Intelligent routing | Least-loaded worker selection based on live heartbeat metrics |
| Automatic failover | Falls back to RabbitMQ transparently when no workers are alive or dispatch fails |
| Worker registry | In-memory registry with TTL-based eviction (15 s) |
| Heartbeat monitoring | Workers report load and status every 5 seconds |
| Retry logic | Failed queue jobs retry up to 3 times before DLQ |
| Dead Letter Queue | Failed jobs are captured for inspection, never silently dropped |
| Graceful shutdown | API and workers drain in-flight work on SIGINT / SIGTERM before exiting |
| Environment-driven config | No hardcoded credentials — everything configurable via environment variables |
| Single-command deployment | `docker-compose up --build` starts RabbitMQ, API, and worker with health-checked ordering |
| Multi-worker | Scale horizontally by starting additional worker instances — each self-registers |
| Visibility API | `GET /workers` returns a live JSON snapshot of the fleet with load and status |

---

## Tech Stack

| Component | Technology |
|---|---|
| Language | Go 1.24+ |
| Control plane | gRPC + Protocol Buffers |
| Message broker | RabbitMQ 3.13 (AMQP 0.9.1) |
| Job dispatch | HTTP/JSON |
| Worker coordination | In-memory registry with TTL |
| Infrastructure | Docker Compose (multi-stage builds) |

---

## Project Structure

```
hermes/
├── cmd/
│   ├── api/
│   │   └── main.go               # API server — HTTP :8081 + gRPC :50051
│   └── worker/
│       ├── worker_main/
│       │   └── main.go           # Worker node — dispatch server + consumer
│       └── dlq_consumer/
│           └── main.go           # Dead-letter queue inspector
├── internal/
│   ├── config/
│   │   └── config.go             # Environment-driven configuration
│   ├── jobs/
│   │   └── job.go                # Job struct
│   ├── queue/
│   │   ├── config.go             # RabbitMQ topology config
│   │   ├── queue.go              # Queue interface
│   │   └── rabbitmq.go           # RabbitMQ implementation, DLQ setup, retry loop
│   └── worker/
│       ├── registry.go           # In-memory registry with least-loaded routing
│       └── grpc_server.go        # gRPC handler — Register, Heartbeat, ListWorkers
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

### Option B — Local development

**Prerequisites:** Go 1.24+, Docker (for RabbitMQ)

```bash
# 1. Start RabbitMQ only
docker-compose up -d rabbitmq

# 2. Start the API
go run ./cmd/api/main.go

# 3. Start a worker (separate terminal)
go run ./cmd/worker/worker_main/main.go

# 4. Optionally start a second worker
WORKER_DISPATCH_PORT=9002 go run ./cmd/worker/worker_main/main.go
```

### Test it

```bash
# Check the worker fleet
curl http://localhost:8081/workers

# Submit a job
curl -X POST http://localhost:8081/jobs \
  -H "Content-Type: application/json" \
  -d '{"type":"email","payload":{"to":"user@example.com","subject":"Hello"}}'
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
    │       │
    │       └── error   →  publish to RabbitMQ
    │                          │
    │                      202 { routed: "queue", note: "worker unreachable" }
    │
    └── no workers alive  →  publish to RabbitMQ
                                   │
                               202 { routed: "queue", note: "no alive workers" }
```

---

## Worker Lifecycle

```
startup
   │
   ├─ connect to RabbitMQ (retry loop, up to 10 attempts)
   ├─ start HTTP /dispatch server
   ├─ dial API gRPC :50051
   ├─ Register("<uuid>@<host>:<port>")
   │
   ├─ heartbeat goroutine (every 5 s) ─────────────────────────┐
   │       sends: worker_id, load counter                      │
   │                                                           │
   ├─ RabbitMQ consumer goroutine                              │
   │       │                                                   │
   │       ├─ message received → process → Ack                 │
   │       │                                                   │
   │       └─ failure → increment attempts                     │
   │               │                                           │
   │               ├─ attempts < 3 → Nack (re-queued)          │
   │               │                                           │
   │               └─ attempts == 3 → Dead Letter Queue        │
   │                                                           │
   └─ SIGINT / SIGTERM received                                │
           │                                                   │
           ├─ HTTP server: graceful shutdown (5 s timeout)  ◄──┘
           ├─ RabbitMQ connection: close
           └─ gRPC connection: close
```

---

## Graceful Shutdown

Both the API and worker handle `SIGINT` and `SIGTERM`. On receiving a signal:

- The **API** stops accepting new HTTP requests, waits up to 5 seconds for in-flight handlers to complete, then stops the gRPC server and closes the RabbitMQ connection.
- Each **worker** shuts down its dispatch HTTP server, closes its RabbitMQ consumer, and closes its gRPC connection to the API.

A rolling deployment or `docker-compose down` will not drop in-flight jobs.

---

## Running the DLQ Consumer

To inspect jobs that have exhausted all retries:

```bash
go run ./cmd/worker/dlq_consumer/main.go
```

---

## RabbitMQ Management UI

[http://localhost:15672](http://localhost:15672) — username `guest`, password `guest`

Inspect queue depths, message rates, and dead-lettered jobs here.

---

## Roadmap

- [ ] Web dashboard — real-time worker and job monitoring UI
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