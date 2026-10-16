# Webhook

Released in addons v3.4.2. Tested on core v3.4.2.

Connects one infinimesh namespace to an HTTP endpoint you run. Core publishes `OTA_REQUESTED`, `OTA_TARGET_SET`, `OTA_STATUS_CHANGED`, and device membership events. This addon POSTs each of those once to the namespace `ota.webhook_url`. Your endpoint can POST a target or a report back. A target is written to `desired.ota`, and core then publishes `OTA_TARGET_SET` when `version` changes.

```json
{
  "desired": {"ota": {"version": "1.2.0", "url": "https://files.example/a", "sha256": "<64 lowercase hex>", "size": 4}},
  "reported": {"ota": {"status": "installing", "error": "", "updated_at": "2026-10-15T00:00:00Z"}}
}
```

You need Docker, the `inf` CLI, and core 3.4.2 or newer. The webhook addon reads the namespace with its addon token. The core v3.4.1 image does not serve that read, and the process stops with `could not read the namespace webhook_url`. This addon is not published as an image. Build it from this repository.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `OTA_ADDON_TOKEN` | | plugin bearer |
| `OTA_NAMESPACE` | | one namespace. Use this or `OTA_NAMESPACES`, not both |
| `OTA_NAMESPACES` | | comma separated namespaces for a public plugin |
| `API_URL` | | core gRPC address. `https://api.example` or, with `OTA_ALLOW_INSECURE=1`, `http://api:8000` |
| `OTA_WEBHOOK_URL` | | `ota.webhook_url` for `OTA_NAMESPACE`. `https://hook.example/ota` |
| `OTA_WEBHOOK_URLS` | | `namespace=url` pairs, comma separated, when `OTA_NAMESPACES` is set. Use this or `OTA_WEBHOOK_URL`, not both |
| `OTA_WEBHOOK_SECRET` | | HMAC-SHA256 key. Required. The same value signs outbound posts and checks inbound posts |
| `OTA_WEBHOOK_LISTEN` | `:8080` | address for inbound posts |
| `OTA_ALLOW_INSECURE` | | set to `1` to allow an `http://` `API_URL` or webhook URL. Without it the process stops with `http:// refused without OTA_ALLOW_INSECURE` |
| `OTA_POLL_SECS` | `15` | seconds between retries of a post that got a network error, 408, 429, or 5xx. `0` retries only inside the first attempt |
| `OTA_STATE_FILE` | | file of delivered `event_id` values and posts that still need a retry. A restart does not post an `event_id` again |

At startup the process reads `ota.webhook_url` with the addon token. `OTA_WEBHOOK_URL` has to be that same address, or the process stops with `OTA_WEBHOOK_URL does not match the namespace webhook_url`. A later change made with `inf namespaces update --webhook-url` is used on the next poll, or within 15 seconds when `OTA_POLL_SECS` is `0`. A namespace whose provider is `webhook` cannot be enabled without that field. Core returns `webhook_url is required when provider is webhook`.

## Outbound

Each post is JSON. `event` is `OTA_REQUESTED`, `OTA_TARGET_SET`, `OTA_STATUS_CHANGED`, `DEVICE_ADDED`, `DEVICE_REMOVED`, `TAG_ADDED`, or `TAG_REMOVED`. `event_id` is core's id for that change. A shadow update that is not one of those events is not posted.

```json
{"event_id": 9, "event": "OTA_REQUESTED", "device": "pump", "namespace": "plant", "desired": {"requested_version": "1.4.0"}}
```

`X-Infinimesh-Signature` is `sha256=` and the hex HMAC-SHA256 of the raw body, using `OTA_WEBHOOK_SECRET`. The user agent is `infinimesh-ota-webhook`.

Opening the stream makes core send the current device list as `DEVICE_ADDED` with sequence 0 and a new `event_id`, then it replays events from while the process was down. The process keeps each device's id, title, and tags in `OTA_STATE_FILE`. A list entry is posted only when the device is new or its title or tags changed, and that body has `"snapshot": true`. On a restart the list entry is held until the replay. If the replay has the same device with the same title and tags, only that replayed event is posted. A list entry the replay does not cover is posted when a later event arrives for that namespace, or on the next poll. The same device on the next restart is not posted again. A later `DEVICE_ADDED` is posted when the title or tags changed.

A 2xx is success. The same `event_id` is not posted again. A network error, 408, 429, or 5xx is tried again, up to 6 times, waiting 200ms and then doubling up to 30s. After that the post stays in `OTA_STATE_FILE` and is tried on the next poll. A success after those retries is still one delivery, and the saved device title and tags are updated from that post. A restart does not post that change again.

## Inbound

`POST /` with the same `X-Infinimesh-Signature` header. A bad or missing signature is 401 and writes nothing.

A target sets `desired.ota`:

```json
{"device": "pump", "target": {"version": "1.2.0", "url": "https://files.example/a", "sha256": "<64 lowercase hex>", "size": 4}}
```

A report sets `reported.ota`:

```json
{"device": "pump", "report": {"status": "installing", "error": ""}}
```

`requested_version` is 400 `addon token can write desired ota only`. A key core does not accept, such as `progress`, is 400 `ota field progress is not supported`. `sha256`, `size`, `status`, and `updated_at` use the same sentences as core. A good body is 204. Core publishes `OTA_TARGET_SET` when `desired.ota.version` changes.

## Limits

If the endpoint does not answer, the process logs the failure and keeps running. The post is tried again. A namespace whose provider is not `webhook` is refused by core when this process writes. Core's message is `ota provider does not match the namespace`.

`token is revoked` means the plugin was rotated, its provider changed, or it was deleted. The process logs `token is revoked; run inf plugins rotate and update the secret` and exits non-zero. Run `inf plugins rotate <plugin>`, put the new bearer in `OTA_ADDON_TOKEN`, and start again.

## Run

Log in as root, create the plugin, and turn OTA on for the namespace. The plugin bearer is printed once. `webhook_url` has to be set in the same call. It is the URL this process will POST to.

```bash
inf login --root
inf plugins create --kind ota --provider webhook --title webhook --namespace <namespace>
inf namespaces update <namespace> --ota --provider webhook --webhook-url https://hook.example/ota
```

`ota cannot be enabled until an OTA addon is registered` means the plugin does not exist yet. Create it first.

`webhook_url is required when provider is webhook` means the namespace update left that field empty.

`ota provider requires the addon token` means the call used an account token. `desired.ota.version` is written by this process with `OTA_ADDON_TOKEN`.

```bash
docker compose -f ota-webhook/docker-compose.yml up --build
```

Set `OTA_ADDON_TOKEN`, `OTA_NAMESPACE`, `API_URL`, `OTA_WEBHOOK_URL`, and `OTA_WEBHOOK_SECRET` in that file first. Point `OTA_WEBHOOK_URL` at the endpoint you run. The compose file does not start one. Inbound posts arrive on port 8080.

An account still asks for a version with `inf ota assign`. That stores `requested_version` and publishes `OTA_REQUESTED`. This process POSTs that event. It does not install the bytes itself.

```bash
inf ota assign --namespace <namespace> --device <device> --version 1.2.0
```

Apache-2.0.
