# E-Wallet System

A Go-based e-wallet and payment system, implemented as two independently
deployable microservices. It covers user registration/login, wallet balances,
top-ups, peer-to-peer transfers, payments to merchants, and transaction
history, following the contract in [`openapi.yaml`](./openapi.yaml).

The project started as a single monolith and was decomposed so the
payment flow can scale and evolve on its own.

## Services

| Service | Port | Responsibilities |
|---|---|---|
| **core-service** | `8080` | Users, wallets, top-ups, transfers, transaction history. Issues JWTs. Exposes an internal wallet-debit API. |
| **payment-service** | `8081` | The `POST /transactions/pay` endpoint and the expiring-payment mechanism. Validates JWTs, publishes payment work to RabbitMQ, and calls core-service to move money. |

Each service is its own Go module with its own PostgreSQL database. There is no
API gateway: clients call each service directly on its port. Both services
share the same `JWT_SECRET`, so a token minted by core-service at
`/users/login` is accepted by payment-service.

Payments are processed **asynchronously** via RabbitMQ: `POST /transactions/pay`
returns immediately with status `pending`, and a background consumer in
payment-service performs the wallet debit. See
[Asynchronous payment processing](#asynchronous-payment-processing-rabbitmq)
for the full flow, message format, and queue topology.

### How a payment works

1. The client calls `POST /transactions/pay` on **payment-service** (`:8081`).
2. Payment-service creates a *payment session* valid for 10 minutes, publishes a
   message to `payment.queue`, and immediately responds `202 Accepted` with
   status `pending`.
3. A background consumer in payment-service reads the message, simulates
   processing, re-checks the 10-minute expiry, then calls core-service's
   internal `POST /internal/wallets/debit` (authorized with a shared
   `INTERNAL_API_KEY`), which atomically debits the wallet and records a
   `payment` ledger entry so it shows up in history.
4. The session is marked `completed`, `failed`, or `expired`.
5. A background sweeper also marks any session left pending past 10 minutes as
   `expired` — those are never debited.

Transfers are processed asynchronously too: `POST /transactions/transfer`
returns `202` with status `pending`, and a background worker in core-service
settles the balances and writes the matching ledger entries.

## Directory layout

```
.
├── openapi.yaml              # API contract for both services
├── README.md
├── docker-compose.yml        # orchestrates both services + Postgres + RabbitMQ
├── .env.example              # root compose variables (shared secrets, DB creds)
├── deploy/
│   └── postgres/init/        # SQL run on first Postgres start (creates both DBs)
├── core-service/             # Go module: core-service
│   ├── .env.example
│   ├── Dockerfile            # multi-stage build (golang:1.27-alpine -> alpine)
│   ├── go.mod
│   ├── cmd/server/           # main.go — entrypoint, wiring, graceful shutdown
│   └── internal/
│       ├── config/           # env loading + validation
│       ├── database/         # GORM connection + AutoMigrate
│       ├── model/            # GORM entities + request/response DTOs
│       ├── repository/       # DB access (users, wallets, transactions)
│       ├── service/          # business logic (register, login, topup, transfer, debit, history)
│       ├── security/         # JWT issue/verify, bcrypt helpers
│       ├── middleware/       # JWT auth + internal API-key guard
│       ├── handler/          # Gin HTTP handlers (incl. internal debit)
│       ├── router/           # route wiring (/api/v1 + /internal)
│       └── worker/           # async transfer settlement (time.Ticker)
└── payment-service/          # Go module: payment-service
    ├── .env.example
    ├── Dockerfile            # multi-stage build (golang:1.27-alpine -> alpine)
    ├── go.mod
    ├── cmd/server/           # main.go — entrypoint
    └── internal/
        ├── config/           # env loading + validation (incl. RabbitMQ)
        ├── database/         # GORM connection + AutoMigrate (payment_sessions)
        ├── model/            # PaymentSession entity + DTOs
        ├── repository/       # payment session DB access
        ├── service/          # payment flow + session expiry + message processing
        ├── security/         # JWT verify (shared secret, verify-only)
        ├── middleware/       # JWT auth
        ├── handler/          # Gin HTTP handler for /transactions/pay
        ├── router/           # route wiring
        ├── coreclient/       # HTTP client for core-service's internal debit API
        ├── mq/               # RabbitMQ connection, publisher, consumer, message DTO
        └── worker/           # payment-session expiry sweeper (time.Ticker)
```

> `local/` holds working notes and task descriptions used while building the
> project; it is not part of the runtime.

## Tech stack

- **Go** (1.27)
- **Gin** — HTTP framework
- **GORM** + **PostgreSQL** — persistence (`AutoMigrate` creates the schema)
- **RabbitMQ** (`amqp091-go`) — asynchronous payment processing
- **golang-jwt/jwt** — JWT auth
- **bcrypt** — password hashing
- **Docker** + **Docker Compose** — containerized deployment

## Prerequisites

- Go 1.27+
- PostgreSQL running locally
- RabbitMQ running locally (easiest via Docker — see
  [Asynchronous payment processing](#asynchronous-payment-processing-rabbitmq))
- Two databases, one per service. For example:

  ```sql
  CREATE DATABASE ewallet_core;
  CREATE DATABASE ewallet_payment;
  ```

> Prefer not to install anything locally? Skip straight to
> [Containerized deployment](#containerized-deployment) and run the whole stack
> (both services + Postgres + RabbitMQ) with a single `docker compose up`.

## Configuration

Each service reads its config from environment variables, loading a `.env` file
if present. Copy the example files and adjust values:

```bash
cp core-service/.env.example core-service/.env
cp payment-service/.env.example payment-service/.env
```

Important: the two services must agree on the shared secrets.

- `JWT_SECRET` — must be **identical** in both `.env` files.
- `INTERNAL_API_KEY` — must be **identical** in both `.env` files.
- `payment-service` must point at core-service via `CORE_SERVICE_URL`
  (default `http://localhost:8080`).

| Variable | core-service | payment-service | Notes |
|---|---|---|---|
| `SERVER_PORT` | `8080` | `8081` | Listen port |
| `DB_HOST` / `DB_PORT` | ✓ | ✓ | PostgreSQL host/port |
| `DB_USER` / `DB_PASSWORD` | ✓ | ✓ | Credentials |
| `DB_NAME` | `ewallet_core` | `ewallet_payment` | Separate databases |
| `DB_SSLMODE` | ✓ | ✓ | e.g. `disable` locally |
| `JWT_SECRET` | ✓ | ✓ | **Same value in both** |
| `JWT_EXPIRY` | ✓ | — | e.g. `24h` (core issues tokens) |
| `INTERNAL_API_KEY` | ✓ | ✓ | **Same value in both** |
| `CORE_SERVICE_URL` | — | ✓ | URL of core-service |
| `CORE_SERVICE_TIMEOUT` | — | ✓ | e.g. `5s` |
| `RABBITMQ_URL` | — | ✓ | AMQP URL, e.g. `amqp://guest:guest@localhost:5672/` |
| `RABBITMQ_EXCHANGE` | — | ✓ | Direct exchange name (default `payment.exchange`) |
| `RABBITMQ_QUEUE` | — | ✓ | Main queue name (default `payment.queue`) |
| `RABBITMQ_ROUTING_KEY` | — | ✓ | Routing key (default `payment.process`) |
| `RABBITMQ_DLX` | — | ✓ | Dead-letter exchange (default `payment.dlx`) |
| `RABBITMQ_DLQ` | — | ✓ | Dead-letter queue (default `payment.queue.dlq`) |

## Running locally

Each service is a separate module, so run them in two terminals. Both run
`AutoMigrate` on startup, creating their tables automatically.

First make sure RabbitMQ is running (payment-service fails fast if it can't
connect). The quickest way is Docker:

```powershell
docker compose up -d rabbitmq   # management UI at http://localhost:15672 (guest/guest)
```

**Terminal 1 — core-service (start this first):**

```bash
cd core-service
go run ./cmd/server
```

**Terminal 2 — payment-service:**

```bash
cd payment-service
go run ./cmd/server
```

core-service listens on `http://localhost:8080` and payment-service on
`http://localhost:8081`.

> On Windows PowerShell, `cd` into the folder in each terminal as shown, or use
> `go -C <path> run ./cmd/server` to run without changing directory.

## Endpoints

All paths are under the `/api/v1` base path of their service.

**core-service (`:8080`)**

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/users/register` | — | Register a user (also creates a wallet) |
| POST | `/users/login` | — | Authenticate, returns a JWT |
| GET | `/wallet/balance` | JWT | Current wallet balance |
| POST | `/transactions/topup` | JWT | Credit the wallet |
| POST | `/transactions/transfer` | JWT | Transfer to another user (async, returns `202`) |
| GET | `/transactions/history` | JWT | Paginated transaction history (`?page`, `?limit`) |
| POST | `/internal/wallets/debit` | internal key | Service-to-service wallet debit (used by payment-service) |

**payment-service (`:8081`)**

| Method | Path | Auth | Description |
|---|---|---|---|
| POST | `/transactions/pay` | JWT | Pay a merchant (10-minute payment session) |

## Quick smoke test

```bash
# 1. Register (core-service)
curl -X POST http://localhost:8080/api/v1/users/register
  -H "Content-Type: application/json"
  -d "{\"name\":\"John Doe\",\"email\":\"john@example.com\",\"password\":\"secret123\"}"

# 2. Login -> copy the "token" from the response
curl -X POST http://localhost:8080/api/v1/users/login
  -H "Content-Type: application/json"
  -d "{\"email\":\"john@example.com\",\"password\":\"secret123\"}"

# 3. Top up (core-service)
curl -X POST http://localhost:8080/api/v1/transactions/topup
  -H "Authorization: Bearer <TOKEN>"
  -H "Content-Type: application/json"
  -d "{\"amount\":1000000}"

# 4. Pay a merchant (payment-service, port 8081, same token)
curl -X POST http://localhost:8081/api/v1/transactions/pay
  -H "Authorization: Bearer <TOKEN>"
  -H "Content-Type: application/json"
  -d "{\"merchantID\":\"merchant-123\",\"amount\":25000,\"description\":\"Coffee\"}"

# 5. History (core-service) — the payment appears here
curl http://localhost:8080/api/v1/transactions/history
  -H "Authorization: Bearer <TOKEN>"
```

## Asynchronous payment processing (RabbitMQ)

The payment-service processes payments asynchronously via RabbitMQ instead of
debiting the wallet synchronously inside the HTTP request.

### Flow

1. A client calls `POST /api/v1/transactions/pay` (JWT-protected).
2. payment-service creates a `pending` `PaymentSession` (expires in 10 minutes),
   publishes a `PaymentMessage` to `payment.queue`, and immediately responds
   `202 Accepted` with `status: "pending"`.
3. A background consumer in payment-service reads `payment.queue`, simulates
   processing (`time.Sleep`), checks the 10-minute expiry, then debits the
   wallet by calling core-service `POST /internal/wallets/debit`.
4. The session is updated to `completed`, `failed`, or `expired`.

### Message payload (`payment.queue`)

```json
{
  "payment_id":  "<uuid>",
  "user_id":     "<uuid>",
  "amount":      100.00,
  "merchant_id": "merchant-123",
  "description": "optional",
  "created_at":  "2026-10-13T10:00:00Z"
}
```

`payment_id`, `user_id`, `amount`, and `created_at` (RFC3339) satisfy the task
contract; `merchant_id` and `description` are added so the consumer can perform
the debit.

### Topology

- Direct exchange `payment.exchange`, routing key `payment.process`.
- Main durable queue `payment.queue`, configured with
  `x-dead-letter-exchange = payment.dlx`.
- Dead-letter exchange `payment.dlx` + durable queue `payment.queue.dlq`.
- Messages that fail to decode or hit a transient/unexpected error are
  `nack`ed without requeue, so RabbitMQ routes them to `payment.queue.dlq`.
  Business failures (insufficient funds, wallet not found) mark the session
  `failed` and are acked (not dead-lettered).

All names are configurable via env vars (see `payment-service/.env.example`).

### Trying it

Start RabbitMQ (from the repo root), then run both services as in
[Running locally](#running-locally):

```powershell
docker compose up -d rabbitmq   # management UI: http://localhost:15672 (guest/guest)
```

On startup payment-service connects to RabbitMQ, declares the topology, and
starts the consumer; the queues/exchanges become visible in the UI. Call the
endpoint (replace `$TOKEN` with a JWT from core-service login):

```powershell
curl -X POST http://localhost:8081/api/v1/transactions/pay `
  -H "Authorization: Bearer $TOKEN" `
  -H "Content-Type: application/json" `
  -d '{"merchantID":"merchant-123","amount":100,"description":"test"}'
```

The response is immediate with `status: "pending"`; the consumer processes the
message shortly after and the session transitions to `completed` / `failed` /
`expired`.

### Expiry

The expiry sweeper (every 30s) marks any `pending` session past its 10-minute
window as `expired`. The consumer also re-checks expiry before debiting, so a
message that sits in the queue too long is never debited.

## Containerized deployment

Both services plus their infrastructure (PostgreSQL, RabbitMQ) run via a single
Docker Compose stack.

### Components

| Service          | Image                              | Host port | Purpose                          |
|------------------|------------------------------------|-----------|----------------------------------|
| core-service     | built from `core-service/Dockerfile`     | 8080  | users, wallets, transactions     |
| payment-service  | built from `payment-service/Dockerfile`  | 8081  | payments (async via RabbitMQ)    |
| postgres         | `postgres:16-alpine`               | 5432      | DBs `ewallet_core`, `ewallet_payment` |
| rabbitmq         | `rabbitmq:3-management`            | 5672 / 15672 | AMQP broker + management UI   |

The Dockerfiles use a multi-stage build: `golang:1.27-alpine` compiles a static
binary (`CGO_ENABLED=0`), which is copied into a minimal `alpine:3.20` runtime
image running as a non-root user.

### Configuration

Service hostnames inside the Docker network use compose service names (not
`localhost`). These are injected via `environment:` in `docker-compose.yml`, so
each service's own `.env` file is left untouched for running outside Docker:

- `DB_HOST=postgres`
- `RABBITMQ_URL=amqp://guest:guest@rabbitmq:5672/`
- `CORE_SERVICE_URL=http://core-service:8080` (payment-service -> core-service)

Shared secrets (`JWT_SECRET`, `INTERNAL_API_KEY`) and the Postgres credentials
come from the root `.env` (see root `.env.example`), with safe local-dev
defaults baked into the compose file. Both app databases are created on first
Postgres startup by `deploy/postgres/init/01-create-databases.sql`.

### Startup ordering

`depends_on` with health conditions ensures correct boot order:

- core-service waits for Postgres to be healthy.
- payment-service waits for Postgres + RabbitMQ to be healthy and core-service
  to have started.

### Running

```powershell
# From the repo root
docker compose up --build        # build images and start everything
docker compose up --build -d     # same, detached

docker compose ps                # check status
docker compose logs -f core-service payment-service

docker compose down              # stop and remove containers
docker compose down -v           # also remove the postgres/rabbitmq volumes
```

Once up:

- core-service:        http://localhost:8080
- payment-service:     http://localhost:8081
- RabbitMQ management:  http://localhost:15672 (guest / guest)

Expected startup logs:

```
go-e-wallet-postgres   Healthy
go-e-wallet-rabbitmq   Server startup complete; 5 plugins started  (Healthy)
go-e-wallet-core       core-service: database connected and migrated
go-e-wallet-core       core-service listening on :8080
go-e-wallet-payment    payment-service: database connected and migrated
go-e-wallet-payment    payment-service: connected to rabbitmq
go-e-wallet-payment    payment consumer started on queue "payment.queue"
go-e-wallet-payment    payment-service listening on :8081
```

## Notes

- Schema is managed by GORM `AutoMigrate`; there is no separate migration step.
- Background jobs (transfer settlement, payment expiry) run as in-process
  goroutines driven by `time.Ticker` and stop gracefully on shutdown.
- Payments are processed asynchronously through RabbitMQ; see
  [Asynchronous payment processing](#asynchronous-payment-processing-rabbitmq).
- `.env` files are git-ignored; commit only the `.env.example` templates.
