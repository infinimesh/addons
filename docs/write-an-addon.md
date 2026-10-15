# Write an addon

An addon is a small Go program that logs in to the infinimesh API, follows the devices an account can see, and sends their state somewhere else. This guide shows how to write one with the SDK in [`sdk`](../sdk/README.md), using the same layout as the addons in this repository.

There are two kinds of addon:

- **Shadow addons** stream device shadows straight from the API. Use this for a new sink. `kafka`, `webhook`, `postgres`, `influxdb` and `mqttbridge` work this way.
- **Redis stream sinks** read the Redis stream that `redisstream` writes. Use this when several sinks should share one API connection. `elasticsearch`, `snowflake` and `saphana` work this way.

## What a shadow looks like

Core keeps one shadow per device with three parts:

- **reported**: what the device last sent, with a timestamp
- **desired**: what someone asked the device to do, with a timestamp
- **connection**: whether the device is connected, when that changed, and its MQTT client id

An update can carry any of the three. `sdk/message` turns an update into this JSON, and leaves out the parts that are missing:

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

Use this format when the target takes JSON, so all addons send the same thing.

## Register an OTA plugin

`POST /v1/plugins` requires a root token. On a fresh install the root account is `infinimesh` and the password is `INF_DEFAULT_ROOT_PASS`. The token request sets `inf` to true. A login without it gets 403.

```bash
curl -s -X POST "$API/v1/token" \
  -H 'Content-Type: application/json' \
  -d '{"auth":{"type":"standard","data":["infinimesh","<INF_DEFAULT_ROOT_PASS>"]},"inf":true}'
```

Create the plugin with that token. `kind` is `OTA` and `provider` is `hawkbit`, `mender`, or `webhook`. The response field `token` is returned once and is not stored.

```bash
curl -s -X POST "$API/v1/plugins" \
  -H "Authorization: Bearer <root token>" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Hawkbit","kind":"OTA","provider":"hawkbit","public":true}'
```

The addon sends `Authorization: Bearer <token>` on patches that set `desired.ota`. A later get returns `token_id` and an empty `token`. Deleting the plugin, or changing its provider, invalidates that token. This is not a call the addon SDK makes. A shadow addon still logs in with `USERNAME` and `PASSWORD`. [`ota-hawkbit`](../ota-hawkbit/README.md) uses the plugin bearer as `OTA_ADDON_TOKEN` and does not log in with an account.

## The SDK

| Package | Use it for |
|---|---|
| `sdk/runner` | the whole main loop of a shadow addon: login, token refresh, one shadow stream per namespace that follows added and removed devices (`runner.Serve`), `runnertest` for sink tests |
| `sdk/message` | `message.New` gives a `*message.Message`, `message.Build` gives the device id and the JSON bytes. `Message.Time()` picks the best timestamp. |
| `sdk/batch` | queue rows from all devices and write them in batches, keeping them while the target is down |
| `sdk/config` | read settings: `config.String`, `config.Int`, `config.Bool`, `config.Seconds`, and `config.Env` to swap the environment in tests |
| `sdk/api` | the gRPC client under all of this, if you need more than the runner gives you |
| `sdk/wrappers` | start and stop one worker per device (older per-device path, no addon uses it now) |
| `sdk/stream/consumer` | read the `redisstream` stream, for Redis stream sinks |

`runner.Run` reads these settings for you, and every shadow addon lists them in its README:

| Variable | Default | Meaning |
|---|---|---|
| `USERNAME`, `PASSWORD` | | infinimesh account |
| `API_URL` | | `http://host:port` for plain gRPC, `https://host` or `host:443` for TLS |
| `TOKEN_REFRESH_INTERVAL_SECS` | `3600` | how often to log in again. Each token and its session expire after twice this interval. |
| `DEVICE_REFRESH_INTERVAL_SECS` | `600` | how often to look for new or removed namespaces; devices are picked up at once from the namespace stream |

## A first addon

This addon prints one JSON line per shadow update. Make a folder named after the addon, here `stdout`, and add `stdout/main.go`:

```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/infinimesh/addons/sdk/api"
	"github.com/infinimesh/addons/sdk/config"
	"github.com/infinimesh/addons/sdk/message"
	"github.com/infinimesh/addons/sdk/runner"
	shadowpb "github.com/infinimesh/proto/shadow"
)

func main() {
	onlyReported, err := config.Bool("STDOUT_ONLY_REPORTED", false)
	if err != nil {
		log.Fatal(err)
	}
	runner.Run(func(obj api.Object, s *shadowpb.Shadow) {
		_, body, ok := message.Build(obj, s, onlyReported, time.Now())
		if !ok {
			return
		}
		fmt.Println(string(body))
	})
}
```

Run it against a platform:

```bash
USERNAME=infinimesh PASSWORD=infinimesh API_URL=http://localhost:8000 go run ./stdout
```

`runner.Run` calls your function for one device at a time and in order, but different devices run at the same time. So:

- keep state shared between devices behind a lock, or use `sdk/batch`, which is safe to call from all workers
- a slow call only holds up that one device

## Settings

Read every setting once at start, in a `loadSettings` function that returns an error instead of exiting. That keeps it easy to test. Prefix the names with the addon name, such as `WEBHOOK_URL`, and give every optional setting a default.

```go
type settings struct {
	URL     string
	Timeout time.Duration
}

func loadSettings() (*settings, error) {
	s := &settings{URL: config.Env("MYSINK_URL")}
	if s.URL == "" {
		return nil, fmt.Errorf("MYSINK_URL is required")
	}
	var err error
	if s.Timeout, err = config.Seconds(10*time.Second, "MYSINK_TIMEOUT_SECS"); err != nil {
		return nil, err
	}
	return s, nil
}
```

## Writing in batches

Databases want many rows per write. Put `sdk/batch` between the runner and the database:

```go
b := &batch.Batcher[Row]{
	Size:      500,         // most rows per write
	Interval:  time.Second, // longest wait before a write
	MaxBuffer: 100000,      // oldest rows are dropped past this
	Flush: func(rows []Row) error {
		return db.Insert(rows) // an error keeps the rows for the next try
	},
}
go b.Run()

runner.Run(func(obj api.Object, s *shadowpb.Shadow) {
	if m, ok := message.New(obj, s, false, time.Now()); ok {
		b.Add(toRow(m))
	}
})
```

Return `nil` from `Flush` for data the target will never take, such as a type conflict, and log it. Otherwise that batch is retried until it is pushed out of the buffer and blocks the rows behind it. `influxdb` shows how.

## Retries and errors

- Retry network errors, timeouts and "busy" answers such as HTTP 429 and 5xx, with a growing wait. `webhook` shows how.
- Do not retry answers that say the request itself is wrong, such as HTTP 400. Log them and move on.
- Never call `log.Fatal` in the update function. One bad update must not stop the addon.
- Log one short line per failure with the device id. Do not log payloads or secrets.

## Redis stream sinks

A sink reads the stream that `redisstream` writes, instead of the API:

```go
pool, err := config.RedisPool() // REDIS_ADDR, REDIS_PASSWORD, REDIS_TLS
if err != nil {
	log.Fatal(err)
}
consumer.New(pool).Run(func(e *consumer.DeviceEvent) error {
	return write(e.Object, e.State) // nil acknowledges the entry
})
```

An entry is acknowledged only when the handler returns `nil`, so a failed entry is read again later. The events carry reported state only.

## Files

Copy the layout of an existing addon, for example `webhook`:

```
mysink/
  main.go               wiring: settings, client, runner.Run
  mysink.go             the code that talks to the target
  mysink_test.go        tests
  Dockerfile
  docker-compose.yml
  README.md
```

**Dockerfile.** Build from the repository root so the SDK is in the context:

```dockerfile
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/app ./mysink

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /out/app /app
USER 65532:65532
ENTRYPOINT ["/app"]
```

**docker-compose.yml.** Use `context: ..` and `dockerfile: mysink/Dockerfile`. Put `CHANGEME` and `https://api.example.com` as placeholders. If the target has an official image, start it next to the addon with a comment saying it is only for trying it out.

**README.md.** Use these parts, in this order:

1. one line on what the addon does, then the data format with an example
2. `## Settings`: a table with every variable, its default and its meaning, starting with the four runner settings
3. `## Limits`: what is retried, what is dropped, what is not supported
4. `## Run`: the `docker compose` command
5. `Apache-2.0.`

Then add a row to the addon table in the main [README](../README.md).

## Tests

- Test `loadSettings` by swapping `config.Env` for a map, and put it back with `t.Cleanup`.
- Test the conversion from `message.Message` to your row, point or request on its own, without a network.
- Use `net/http/httptest` for HTTP targets.
- Before you push, run `make commit-check`. It formats the code, runs the tests with the race detector and the fuzz tests, and runs CodeQL.

To try the addon end to end, run core locally (see the [infinimesh docs](https://github.com/infinimesh/.github)), create a device with `inf`, send a few states and check that they arrive.

Apache-2.0.
