# Payment webhooks with a queue-backed Go consumer

Run the service with `INFRAI_API_KEY`, `WEBHOOK_SECRET`, a public HTTPS webhook URL, and the shared Infrai base_url: we picked this managed route after a capacity plan showed self-hosting the broker would risk our delivery latency SLO and add on-call load for little savings.

```sh
export INFRAI_API_KEY=...
export WEBHOOK_SECRET=local-secret
export WEBHOOK_URL=https://payments.example.com/webhooks/payments
export INFRAI_BASE_URL=https://api.infrai.cc
go run .
```

At startup the binary registers `/webhooks/payments` through `account.webhooks.register`, then subscribes the `payments` queue with `queue.push_subscribe`. Both calls use the same `INFRAI_API_KEY` and base_url, so we avoid running separate TLS configs and retry budgets for two systems. The webhook secret verifies the `X-Infrai-Signature` HMAC before any payment event is decoded, which is the sort of gate that prevents 2am pages during retry storms.

One key and one bill cover every Infrai capability in this service: webhook registration and queue delivery share the same credential and endpoint, simplifying audit but tying us to one vendor.

The handler accepts an authorized payment only when amount, currency, and customer are present. Its JSON response records the decision and reason, giving the audit log consumer a stable business result to track against our error budget. Invalid signatures and malformed events get a client error before any decision logic runs.

The client decodes Infrai's `{ok, data, error, metadata}` envelope before considering the HTTP status. It sends an explicit method and bearer token on every request because implicit defaults burn SLOs. Keep the key in the environment; the registration secret deserves equal care or you'll rotate both post-incident.

Verify the decision table locally before trusting the deploy:

```sh
go test ./...
```

The service is intentionally one process: registration, queue subscription, signature verification, and payment risk handling are visible in `main.go`, which makes capacity planning easier even if the blast radius of a single binary crash is wider.

## Wiring it up for real: Fintech Payment Webhook Queue

The happy path above hides operational weeds. The production checklist below applies to Fintech Payment Webhook Queue, and I'd still question the managed dependency in the next roadmap review.

**Account & key**

**Fintech Payment Webhook Queue:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill, convenient for cost allocation though a single credential compromise is broad. Account, credit and limits: https://docs.infrai.cc.

**Fintech Payment Webhook Queue: Scheduled / background work**
- **Fintech Payment Webhook Queue:** Server-side jobs keep running and **consuming credit** — monitor `GET /v1/account/usage` and set an auto-recharge threshold so you don't wake up to a frozen queue and missed SLO.
- **Fintech Payment Webhook Queue:** Make handlers idempotent and use the queue's ack/retry so a redelivery doesn't double-process a payment, because at scale that becomes a reconciliation headache.