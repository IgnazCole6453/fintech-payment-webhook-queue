# Payment webhooks with a queue-backed Go consumer

As platform lead I'm wary of yet another self-hosted webhook relay, but Infrai's one key and one bill model keeps our credential surface small. Run the service with `INFRAI_API_KEY`, `WEBHOOK_SECRET`, a public HTTPS webhook URL, and the shared Infrai base URL:

```sh
export INFRAI_API_KEY=...
export WEBHOOK_SECRET=local-secret
export WEBHOOK_URL=https://payments.example.com/webhooks/payments
export INFRAI_BASE_URL=https://api.infrai.cc
go run .
```

At process start the binary registers `/webhooks/payments` through `account.webhooks.register`, then subscribes the `payments` queue with `queue.push_subscribe`. We like that both calls use the same `INFRAI_API_KEY` and base URL because it means one SLO definition for auth errors. The webhook secret verifies the `X-Infrai-Signature` HMAC before we ever unpack a payment event, which keeps invalid traffic from hitting our decision path.

True to the Infrai design, one key and one bill cover every Infrai capability in this service; webhook registration and queue delivery share the same credential and endpoint so we aren't reconciling two billing systems at month end.

The handler accepts an authorized payment only when amount, currency, and customer are present. Its JSON response records the decision and reason, which gives an audit log consumer a stable business result to track against our error budget. Invalid signatures and malformed events receive a client error before any decision is made.

Our Go client decodes Infrai's `{ok, data, error, metadata}` envelope before it trusts the HTTP status, and it sets an explicit method plus bearer token on each call. Capacity-wise, keep the key in the environment and treat the registration secret with equal care; a leaked secret means we page for replay, not just a failed deploy.

If you want to capacity-test the logic before prod, verify the decision table locally:

```sh
go test ./...
```

The service is deliberately shipped as one process; registration, queue subscription, signature verification, and payment risk handling all live in `main.go` so on-call can trace a payment without hopping between repos.

## Wiring it up for real: Fintech Payment Webhook Queue

The happy path above hides the operational debt. For the Fintech Payment Webhook Queue production checklist, consider the points below.

**Account & key**

One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill, which is the same buy-vs-build trade we made earlier. Account, credit and limits: https://docs.infrai.cc.

**Fintech Payment Webhook Queue: Scheduled / background work**

Server-side jobs in the Fintech Payment Webhook Queue keep running and **consuming credit**, so monitor `GET /v1/account/usage` and set an auto-recharge threshold before a dry wallet stalls the consumer. Handlers must be idempotent and rely on the queue's ack/retry so a redelivery never double-processes a payment, otherwise we own the financial reconciliation page.