# Asynchronous Payment Processing (RabbitMQ)

The payment-service processes payments asynchronously via RabbitMQ instead of
debiting the wallet synchronously inside the HTTP request.

## Flow

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

## Topology

- Direct exchange `payment.exchange`, routing key `payment.process`.
- Main durable queue `payment.queue`, configured with
  `x-dead-letter-exchange = payment.dlx`.
- Dead-letter exchange `payment.dlx` + durable queue `payment.queue.dlq`.
- Messages that fail to decode or hit a transient/unexpected error are
  `nack`ed without requeue, so RabbitMQ routes them to `payment.queue.dlq`.
  Business failures (insufficient funds, wallet not found) mark the session
  `failed` and are acked (not dead-lettered).

All names are configurable via env vars (see `.env.example`).

## Running locally

1. Start RabbitMQ (from the repo root):

   ```powershell
   docker compose up -d
   ```

   Management UI: http://localhost:15672 (guest / guest).

2. Start core-service (port 8080) and payment-service (port 8081):

   ```powershell
   # terminal 1
   cd core-service; go run ./cmd/server

   # terminal 2
   cd payment-service; go run ./cmd/server
   ```

   On startup payment-service connects to RabbitMQ, declares the topology, and
   starts the consumer. The queues/exchanges become visible in the UI.

3. Call the endpoint (replace `$TOKEN` with a JWT from core-service login):

   ```powershell
   curl -X POST http://localhost:8081/api/v1/transactions/pay `
     -H "Authorization: Bearer $TOKEN" `
     -H "Content-Type: application/json" `
     -d '{"merchantID":"merchant-123","amount":100,"description":"test"}'
   ```

   The response is immediate with `status: "pending"`; the consumer processes
   the message shortly after and the session transitions to `completed` /
   `failed` / `expired`.

## Expiry

The existing expiry sweeper (every 30s) marks any `pending` session past its
10-minute window as `expired`. The consumer also re-checks expiry before
debiting, so a message that sits in the queue too long is never debited.
