# Infinimesh addons

Module `github.com/infinimesh/addons`. Connectors that sit beside [core](https://github.com/infinimesh/core). They are not part of the platform process.

Apache-2.0.

## Layout

| Directory | What it does |
|---|---|
| `sdk` | API client, object iteration, Redis stream consumer |
| `redisstream` | Read the platform API and publish device events on Redis |
| `elasticsearch` | Redis stream to the Elasticsearch index `infinimesh` |
| `snowflake` | Redis stream to Snowflake database `infinimesh`, table `device_states` |
| `saphana` | Redis stream to SAP HANA |
| `timeseries` | Platform API to RedisTimeSeries, with a Grafana compose file |
| `cloudconnect` | Platform API to a CSV spool, then S3, GCS, or Azure Blob |

Sinks (`elasticsearch`, `snowflake`, `saphana`) consume the stream from `redisstream`. `timeseries` and `cloudconnect` call the API themselves.

## Run

From this repository, with `API_URL` pointing at a running platform:

```bash
docker compose -f redisstream/docker-compose.yml up --build
docker compose -f elasticsearch/docker-compose.yml up --build
```

Each compose file's build context is the repository root. Set `USERNAME`, `PASSWORD`, and `API_URL` in that file. Placeholders are `CHANGEME` and `https://api.example.com`.

## Talking to core

The SDK uses the core v3 gRPC API through the generated clients in [infinimesh/proto](https://github.com/infinimesh/proto), the same way `inf` does.

- `USERNAME` and `PASSWORD` are a standard infinimesh account. A fresh core install has the root account `infinimesh` with the password from `INF_DEFAULT_ROOT_PASS`. A separate read only account is better for addons.
- `API_URL` is the gRPC address. `https://api.example.com` uses TLS on port 443, which the core compose setup routes to the api service. `http://api.example.com:8000` uses plain gRPC on the `grpc` entry point, or `http://api:8000` from inside the core compose network. With no scheme, TLS is used.
- Addons see every device the account can see. For each device they ask core for a short device token with read access and stream its shadow. Only the reported state is used. The v3 shadow has no version, so the `version` field the sinks store is empty.

`SNOWFLAKE_INITDB=true` creates the Snowflake database and table. Cloud Connect commands are `cloudconnect/csvwriter`, `s3`, `cloudstorage`, and `blob`.

## Checks

`make commit-check` runs `fmt`, `race`, `test-fuzz`, and `code-ql-gate`. `fmt` writes Go files that `gofmt` would change. `code-ql` runs the CodeQL CLI and writes SARIF under `.tmp/codeql/`. The gate fails when a result is level `error`, ignoring paths under `vendor`. `jq` is required. GitHub Actions runs the same race and fuzz steps, and CodeQL on pull requests.

## Build

```bash
go test ./...
go build -o redisstream ./redisstream
```
