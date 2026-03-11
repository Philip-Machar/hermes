# Hermes

A distributed job processing system built with Go, featuring a gRPC control plane, direct HTTP job dispatch, RabbitMQ-backed reliability, and a real-time worker registry.

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![RabbitMQ](https://img.shields.io/badge/RabbitMQ-3.13-FF6600?style=flat&logo=rabbitmq)](https://www.rabbitmq.com/)
[![gRPC](https://img.shields.io/badge/gRPC-1.78-244c5a?style=flat&logo=grpc)](https://grpc.io/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

---

## Overview

Hermes is a production-grade distributed job queue designed around a **control plane / data plane** separation. Workers self-register via gRPC, send continuous heartbeats, and receive jobs either through direct HTTP dispatch or via a durable RabbitMQ queue as a fallback. The system is built to be operationally simple — no external coordination service required.

---

## Screenshots

> _Real-time worker and job monitoring dashboard_

<!-- Add after dashboard is built -->
![Dashboard](docs/screenshots/dashboard.png)

> _Landing page_

<!-- Add after landing page is built -->
![Landing page](docs/screenshots/landing.png)

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         CLIENT                              │
│              curl / HTTP / Any HTTP client                  │
└───────────────────────────┬─────────────────────────────────┘
                            │  HTTP
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                      API SERVER :8081                       │
│                                                             │
│   POST /jobs ──► Registry.PickWorker()                      │
│                         │                                   │
│                  worker alive?                              │
│                  ┌──────┴──────┐                            │
│                 YES            NO                           │
│                  │              │                           │
│                  ▼              ▼                           │
│           HTTP Dispatch    RabbitMQ Queue                   │
│                                                             │
│   GET /workers ──► Registry.List() snapshot                 │
│                                                             │
│   gRPC :50051 ◄── Workers (Register / Heartbeat)            │
└───────────┬─────────────────────────┬───────────────────────┘
            │ gRPC                    │ HTTP Dispatch
            │ (Register/Heartbeat)    │ POST /dispatch
            ▼                        ▼
┌───────────────────┐      ┌───────────────────┐
│   WORKER NODE 1   │      │   WORKER NODE 2   │
│   :9001           │      │   :9002           │
│                   │      │                   │
│  ┌─────────────┐  │      │  ┌─────────────┐  │
│  │  Heartbeat  │  │      │  │  Heartbeat  │  │
│  │  (5s tick)  │  │      │  │  (5s tick)  │  │
│  └─────────────┘  │      │  └─────────────┘  │
│                   │      │                   │
│  ┌─────────────┐  │      │  ┌─────────────┐  │
│  │  RabbitMQ   │  │      │  │  RabbitMQ   │  │
│  │  Consumer   │  │      │  │  Consumer   │  │
│  └─────────────┘  │      │  └─────────────┘  │
└───────────────────┘      └───────────────────┘
            │                        │
            └───────────┬────────────┘
                        │ AMQP consume
                        ▼
        ┌───────────────────────────┐
        │        RabbitMQ           │
        │   jobs queue + DLQ        │
        │   :5672  │  UI :15672     │
        └───────────────────────────┘
```

### Control Plane (gRPC — Worker → API)
Workers initiate all control traffic. On startup each worker dials the API's gRPC server, registers itself with its dispatch address, then enters a heartbeat loop. The API maintains an in-memory registry of alive workers and removes any that haven't heartbeated within 15 seconds.

### Data Plane (HTTP — API → Worker)
When a job arrives the API picks an alive worker from the registry and POSTs the job directly to that worker's `/dispatch` endpoint. If no workers are alive, or if dispatch fails, the job is published to RabbitMQ so it will be processed as soon as a worker comes up.

### Reliability Layer (RabbitMQ)
Every worker also consumes from RabbitMQ. Jobs that enter the queue are retried up to 3 times. After 3 failed attempts they are moved to a Dead Letter Queue (DLQ) for inspection.

---

## Features

- **Direct dispatch** — API pushes jobs straight to a worker over HTTP; no queue round-trip when workers are available
- **Automatic failover** — falls back to RabbitMQ transparently when no workers are alive
- **Worker registry** — real-time in-memory registry with TTL-based expiry (15s)
- **Heartbeat monitoring** — workers report load status (idle / busy) every 5 seconds
- **Retry logic** — failed queue jobs retry up to 3 times before moving to DLQ
- **Dead Letter Queue** — permanent failures are captured for inspection, never silently dropped
- **Multi-worker** — run as many worker instances as needed; each registers independently
- **Visibility API** — `GET /workers` gives a live JSON snapshot of the fleet

---

## Tech Stack

| Component | Technology |
|-----------|-----------|
| Language | Go 1.24+ |
| Control plane RPC | gRPC + Protocol Buffers |
| Message broker | RabbitMQ 3.13 (AMQP 0.9.1) |
| Job dispatch | HTTP/JSON |
| Worker coordination | In-memory registry with TTL |
| Infrastructure | Docker Compose |

---

## Project Structure

```
Hermes/
├── cmd/
│   ├── api/
│   │   └── main.go              # API server — HTTP + gRPC
│   └── worker/
│       ├── worker_main/
│       │   └── main.go          # Worker node
│       └── dlq_consumer/
│           └── main.go          # Dead-letter queue inspector
├── internal/
│   ├── jobs/
│   │   └── job.go               # Job struct
│   ├── queue/
│   │   ├── config.go            # RabbitMQ config
│   │   ├── queue.go             # Queue interface
│   │   └── rabbitmq.go          # RabbitMQ implementation + DLQ setup
│   └── worker/
│       ├── registry.go          # In-memory worker registry
│       └── grpc_server.go       # gRPC handler (Register, Heartbeat, ListWorkers)
├── proto/
│   ├── worker.proto             # Protobuf definitions
│   └── workerpb/
│       ├── worker.pb.go         # Generated message types
│       └── worker_grpc.pb.go    # Generated gRPC stubs
├── scripts/
│   ├── publisher/publish.go     # Manual job publisher (dev utility)
│   └── consumer/consume.go      # Manual queue consumer (dev utility)
├── docker-compose.yml
├── go.mod
└── README.md
```

---

## Quick Start

### Prerequisites

- [Go 1.24+](https://golang.org/dl/)
- [Docker](https://www.docker.com/) (for RabbitMQ)

### 1. Start RabbitMQ

```bash
docker-compose up -d
```

### 2. Start the API server

```bash
go run ./cmd/api/main.go
```

```
gRPC server listening on :50051...
API listening on :8081...
```

### 3. Start a worker

```bash
go run ./cmd/worker/worker_main/main.go
```

```
HTTP dispatch server listening on :9001
Worker registered | id=<uuid> | dispatch=http://localhost:9001
Worker ready — waiting for jobs
```

### 4. Run a second worker (optional)

```bash
go run ./cmd/worker/worker_main/main.go --port 9002
```

### 5. Submit a job

```bash
curl -X POST http://localhost:8081/jobs \
  -H "Content-Type: application/json" \
  -d '{"type":"email","payload":{"to":"user@example.com","subject":"Hello"}}'
```

**Response:**
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

### 6. Inspect the worker fleet

```bash
curl http://localhost:8081/workers
```

```json
{
  "count": 1,
  "workers": [
    {
      "worker_id": "8a4605e1-cd04-42d3-8df7-2a3401b47546",
      "last_seen_unix": 1741614789,
      "load": 0,
      "status": "idle"
    }
  ]
}
```

---

## API Reference

### `POST /jobs`

Submit a job for processing.

**Request body:**
```json
{
  "type": "string",
  "payload": {}
}
```

**Response `202 Accepted` — direct dispatch:**
```json
{
  "job": { "id": "...", "type": "...", "payload": {}, "attempts": 0 },
  "routed": "direct",
  "worker_id": "..."
}
```

**Response `202 Accepted` — queued (no alive workers):**
```json
{
  "job": { "id": "...", "type": "...", "payload": {}, "attempts": 0 },
  "routed": "queue",
  "note": "no alive workers, job queued"
}
```

---

### `GET /workers`

Returns a live snapshot of all registered workers.

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
      "load": 1,
      "status": "busy"
    }
  ]
}
```

---

## Configuration

| Setting | Location | Default | Description |
|---------|----------|---------|-------------|
| RabbitMQ URL | `cmd/api/main.go` | `amqp://guest:guest@localhost:5672/` | AMQP connection string |
| API HTTP port | `cmd/api/main.go` | `:8081` | HTTP API port |
| gRPC port | `cmd/api/main.go` | `:50051` | Worker control plane port |
| Worker dispatch port | `--port` flag | `9001` | Per-worker HTTP dispatch port |
| Heartbeat interval | `cmd/worker/worker_main/main.go` | `5s` | How often workers heartbeat |
| Worker TTL | `internal/worker/registry.go` | `15s` | Time before a silent worker is removed |
| Max job attempts | `cmd/worker/worker_main/main.go` | `3` | Retries before DLQ |

---

## How Job Routing Works

```
POST /jobs
    │
    ▼
Registry.PickWorker()
    │
    ├── worker found ──► HTTP POST to worker /dispatch
    │                         │
    │                    success ──► 202 { routed: "direct" }
    │                         │
    │                    failure ──► publish to RabbitMQ
    │                                    │
    │                              202 { routed: "queue" }
    │
    └── no workers ──► publish to RabbitMQ
                            │
                       202 { routed: "queue" }
```

---

## Worker Lifecycle

```
startup
   │
   ├─ connect to RabbitMQ
   ├─ start HTTP dispatch server
   ├─ connect to API gRPC :50051
   ├─ Register("<uuid>@localhost:<port>")
   │
   ├─ heartbeat loop (every 5s) ─────────────────────────────┐
   │                                                         │
   └─ RabbitMQ consumer loop                                 │
           │                                                 │
           ├─ job arrives ──► process ──► Ack                │
           │                                                 │
           └─ job fails ──► increment attempts               │
                   │                                         │
                   ├─ attempts < 3 ──► Nack + re-publish     │
                   │                                         │
                   └─ attempts == 3 ──► DLQ                  │
                                                             │
                                        ◄────────────────────┘
```

---

## Running the DLQ Consumer

To inspect jobs that have exhausted all retries:

```bash
go run ./cmd/worker/dlq_consumer/main.go
```

---

## RabbitMQ Management UI

Access the RabbitMQ dashboard at [http://localhost:15672](http://localhost:15672)

- **Username:** `guest`
- **Password:** `guest`

You can inspect queue depths, message rates, and dead-lettered jobs here.

---

## Roadmap

- [ ] Web dashboard — real-time worker and job monitoring UI
- [ ] Landing page
- [ ] Docker multi-stage build + single `docker-compose up` startup
- [ ] Metrics endpoint (Prometheus-compatible)
- [ ] Job status tracking (pending / running / complete / failed)
- [ ] Priority queues
- [ ] Distributed registry (Redis) for multi-host deployments

---

## Author

Built by **Philip Machar**

---

## License

MIT