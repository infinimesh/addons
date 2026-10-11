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

`SNOWFLAKE_INITDB=true` creates the Snowflake database and table. Cloud Connect commands are `cloudconnect/csvwriter`, `s3`, `cloudstorage`, and `blob`.

## Build

```bash
go test ./...
go build -o redisstream ./redisstream
```
