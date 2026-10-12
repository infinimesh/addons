# Webhook

Streams device shadows from the infinimesh API to an HTTP endpoint. Every shadow update becomes one `POST` with a JSON body:

```json
{
  "device": "f3913526-38d9-4bf2-a60d-b616d6ec8dcc",
  "title": "pump-7",
  "reported": {"timestamp": "2026-10-12T08:19:51.617Z", "data": {"temp": 20, "hum": 40}},
  "desired": {"timestamp": "2026-10-12T08:10:02.100Z", "data": {"fan": "on"}},
  "connection": {"connected": true, "timestamp": "2026-10-12T08:00:00Z", "client_id": "pump-7"},
  "sent_at": "2026-10-12T08:19:51.618Z"
}
```

Parts that are not in the update are left out. This is the same format the Kafka addon writes.

Headers:

- `Content-Type: application/json`
- `X-Infinimesh-Device`: the device id
- `X-Infinimesh-Signature: sha256=<hex>`: the HMAC-SHA256 of the raw body with `WEBHOOK_SECRET`, sent only when a secret is set

To check a request, compute the HMAC of the body bytes as received and compare it with a constant time compare, for example `hmac.compare_digest` in Python or `hmac.Equal` in Go.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `USERNAME`, `PASSWORD` | | infinimesh account |
| `API_URL` | | infinimesh gRPC address, see the main README |
| `TOKEN_REFRESH_INTERVAL_SECS` | `3600` | how often to log in again |
| `DEVICE_REFRESH_INTERVAL_SECS` | `600` | how often to look for new or removed devices. The older name `OBJECT_REFRESH_INTERVAL_SECS` also works. |
| `WEBHOOK_URL` | | `http` or `https` URL to post to |
| `WEBHOOK_SECRET` | | key for the signature header. Empty means no signature. |
| `WEBHOOK_TIMEOUT_SECS` | `10` | timeout for one request, including reading the response |
| `WEBHOOK_MAX_RETRIES` | `5` | extra tries after the first one fails |
| `WEBHOOK_BACKOFF_MS` | `500` | wait before the first retry. It doubles after each retry. |
| `WEBHOOK_MAX_BACKOFF_SECS` | `30` | longest wait between retries |
| `WEBHOOK_ONLY_REPORTED` | `false` | send only reported state, no desired or connection state |

## Limits

- Any 2xx answer counts as delivered. Network errors, timeouts, 408, 429 and 5xx are retried. Other answers, such as 400 or 401, are logged and the update is dropped.
- After the last retry the update is dropped and logged. There is no disk queue, so updates are lost if the endpoint stays down.
- Each device sends in order, one request at a time. A slow endpoint delays later updates of that device only.
- The signature covers the body only. Use the `sent_at` field if you want to reject old requests.

## Run

```bash
docker compose -f webhook/docker-compose.yml up --build
```

Set `USERNAME`, `PASSWORD`, `API_URL`, `WEBHOOK_URL` and `WEBHOOK_SECRET` first.

Apache-2.0.
