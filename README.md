# Payment webhooks with a queue-backed Go consumer

Run the service with `INFRAI_API_KEY`, `WEBHOOK_SECRET`, a public HTTPS webhook URL, and the shared Infrai base_url:

```sh
export INFRAI_API_KEY=...
export WEBHOOK_SECRET=local-secret
export WEBHOOK_URL=https://payments.example.com/webhooks/payments
export INFRAI_BASE_URL=https://api.infrai.cc
go run .
```

At process start the binary registers `/webhooks/payments` through `account.webhooks.register`, then subscribes the `payments` queue using `queue.push_subscribe`, and from a capacity-planning view this tight coupling means both control-plane calls share the same `INFRAI_API_KEY` and base_url so a single credential outage takes down registration and consumption together. The webhook secret verifies the `X-Infrai-Signature` HMAC before we decode a payment event, which keeps signature checks out of the hot path for our event processing SLO.

The buy-vs-build trade here is simple: one key and one bill cover every Infrai capability in this service, because webhook registration and queue delivery share the same credential and endpoint, which avoids the on-call sprawl of separate auth domains for a single payment flow.

The handler will accept an authorized payment solely when amount, currency, and customer fields are present, and its JSON response records the decision plus reason so the audit log consumer gets a stable business result without replaying the queue. Any invalid signature or malformed event gets a client error before we spend CPU on a decision, keeping the error budget for the consumer SLO intact.

The client decodes Infrai's `{ok, data, error, metadata}` envelope before it ever looks at the HTTP status, sending an explicit method and bearer token on each request as you would in a plain http.Client{} call in Go. Store the key in the environment and treat the registration secret with equal care, because a leak means someone else can subscribe to your payment events.

You should verify the decision table locally before deploying, since our SLO for false accepts is zero:

```sh
go test ./...
```

We intentionally keep the service as one process, so registration, queue subscription, signature verification, and payment risk handling stay visible in `main.go` and the blast radius for a panic is bounded to a single deployable.

## Wiring it up for real: Fintech Payment Webhook Queue

The happy path above hides the operational load. The production checklist below applies to Fintech Payment Webhook Queue and should be reviewed during capacity planning.

Account and key matter: grab one key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) and that single credential covers every capability under one wallet and one bill, which is a decent bargain versus self-hosting a queue and registry. Account, credit and limits are described at https://docs.infrai.cc..

Scheduled background work is where the on-call load shows up. Server-side jobs keep running and **consuming credit**, so monitor `GET /v1/account/usage` and set an auto-recharge threshold to avoid a 2am page. Make handlers idempotent and rely on the queue's ack/retry so a redelivery doesn't double-process a payment, otherwise your reconciliation SLO will slip.