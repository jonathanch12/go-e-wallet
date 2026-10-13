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
| **payment-service** | `8081` | The `POST /transactions/pay` endpoint and the expiring-payment mechanism. Validates JWTs and calls core-service to move money. |

Each service is its own Go module with its own PostgreSQL database. There is no
API gateway: clients call each service directly on its port. Both services
share the same `JWT_SECRET`, so a token minted by core-service at
`/users/login` is accepted by payment-service.

### How a payment works

1. The client calls `POST /transactions/pay` on **payment-service** (`:8081`).
2. Payment-service creates a *payment session* valid for 10 minutes.
3. It calls core-service's internal `POST /internal/wallets/debit` (authorized
   with a shared `INTERNAL_API_KEY`), which atomically debits the wallet and
   records a `payment` ledger entry so it shows up in history.
4. On success the session is marked `completed` and the new balance is returned.
5. A background sweeper marks any session left pending past 10 minutes as
   `expired` — those are never debited.

Transfers are processed asynchronously: `POST /transactions/transfer` returns
`202` with status `pending`, and a background worker in core-service settles
the balances and writes the matching ledger entries.

## Directory layout

```
.
├── openapi.yaml              # API contract for both services
├── README.md
├── core-service/             # Go module: core-service
│   ├── .env.example
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
    ├── go.mod
    ├── cmd/server/           # main.go — entrypoint
    └── internal/
        ├── config/           # env loading + validation
        ├── database/         # GORM connection + AutoMigrate (payment_sessions)
        ├── model/            # PaymentSession entity + DTOs
        ├── repository/       # payment session DB access
        ├── service/          # payment flow + session expiry
        ├── security/         # JWT verify (shared secret, verify-only)
        ├── middleware/       # JWT auth
        ├── handler/          # Gin HTTP handler for /transactions/pay
        ├── router/           # route wiring
        ├── coreclient/       # HTTP client for core-service's internal debit API
        └── worker/           # payment-session expiry sweeper (time.Ticker)
```

> `local/` holds working notes and task descriptions used while building the
> project; it is not part of the runtime.

## Tech stack

- **Go** (1.27)
- **Gin** — HTTP framework
- **GORM** + **PostgreSQL** — persistence (`AutoMigrate` creates the schema)
- **golang-jwt/jwt** — JWT auth
- **bcrypt** — password hashing

## Prerequisites

- Go 1.27+
- PostgreSQL running locally
- Two databases, one per service. For example:

  ```sql
  CREATE DATABASE ewallet_core;
  CREATE DATABASE ewallet_payment;
  ```

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

## Running locally

Each service is a separate module, so run them in two terminals. Both run
`AutoMigrate` on startup, creating their tables automatically.

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

## Notes

- Schema is managed by GORM `AutoMigrate`; there is no separate migration step.
- Background jobs (transfer settlement, payment expiry) run as in-process
  goroutines driven by `time.Ticker` and stop gracefully on shutdown.
- `.env` files are git-ignored; commit only the `.env.example` templates.
