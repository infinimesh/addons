# Infinimesh addons

Module `github.com/infinimesh/addons`. Connectors that sit beside [core](https://github.com/infinimesh/core). They are not part of the platform process.

Apache-2.0.

## Layout

| Addon | What it does |
|---|---|
| [`timeseries`](timeseries/README.md) | API to RedisTimeSeries, with Grafana |
| [`kafka`](kafka/README.md) | API to a Kafka or KafScale topic, JSON keyed by device id |
| [`ota-hawkbit`](ota-hawkbit/README.md) | hawkBit Management API for namespace OTA |
| [`ota-mender`](ota-mender/README.md) | Mender open source Management API for namespace OTA |
| [`ota-webhook`](ota-webhook/README.md) | Signed HTTP posts for namespace OTA |
| [`webhook`](webhook/README.md) | API to an HTTP endpoint, one signed JSON POST per shadow update |
| [`postgres`](postgres/README.md) | API to a Postgres table, optionally a TimescaleDB hypertable |
| [`influxdb`](influxdb/README.md) | API to InfluxDB 2, InfluxDB Cloud or InfluxDB 3, one point per shadow update |
| [`mqttbridge`](mqttbridge/README.md) | API to an existing MQTT broker, JSON per shadow update on a topic template |
| [`cloudconnect`](cloudconnect/README.md) | API to CSV files, then S3, Google Cloud Storage or Azure Blob |
| [`redisstream`](redisstream/README.md) | API to the Redis stream `objects`, read by the sinks below |
| [`elasticsearch`](elasticsearch/README.md) | Redis stream to the Elasticsearch index `infinimesh` |
| [`snowflake`](snowflake/README.md) | Redis stream to the Snowflake table `infinimesh.device_states` |
| [`saphana`](saphana/README.md) | Redis stream to the SAP HANA table `devices` |

[`sdk`](sdk/README.md) holds the shared API client, the device worker manager, the shadow runner, the JSON message and the batcher used by the shadow addons, and the Redis stream consumer.

To write your own addon, see [docs/write-an-addon.md](docs/write-an-addon.md).

## Run

From this repository, with `API_URL` pointing at a running platform:

```bash
docker compose -f timeseries/docker-compose.yml up --build
```

The sink compose files (`elasticsearch`, `snowflake`, `saphana`) already start `redisstream`, so you do not run it on its own as well.

Each compose file's build context is the repository root. Set `USERNAME`, `PASSWORD`, and `API_URL` in that file. Placeholders are `CHANGEME` and `https://api.example.com`.

## Talking to core

The SDK uses the core v3 gRPC API through the generated clients in [infinimesh/proto](https://github.com/infinimesh/proto), the same way `inf` does.

- `USERNAME` and `PASSWORD` are a standard infinimesh account. A fresh core install has the root account `infinimesh` with the password from `INF_DEFAULT_ROOT_PASS`. A separate read only account is better for addons. Creating a plugin, including an OTA plugin, needs a root token from `POST /v1/token`. See [Write an addon](docs/write-an-addon.md#register-an-ota-plugin).
- `ota-hawkbit`, `ota-mender`, and `ota-webhook` do not use `USERNAME` or `PASSWORD`. They send `OTA_ADDON_TOKEN`, the bearer from `inf plugins create`.
- `API_URL` is the gRPC address. `https://api.example.com` uses TLS on port 443, which the core compose setup routes to the api service. `http://api.example.com:8000` uses plain gRPC on the `grpc` entry point, or `http://api:8000` from inside the core compose network. With no scheme, TLS is used.
- Addons see every device the account can see. For each device they ask core for a short device token with read access and stream its shadow. `webhook`, `postgres`, `kafka`, and `mqttbridge` also send the desired and connection state unless their `*_ONLY_REPORTED` setting is `true`. `influxdb` adds the desired state with `INFLUX_INCLUDE_DESIRED=true`. `timeseries`, `redisstream`, and Cloud Connect use only the reported state. The v3 shadow has no version, so the `version` field the sinks store is empty.

`SNOWFLAKE_INITDB=true` creates the Snowflake database and table. Cloud Connect commands are `cloudconnect/csvwriter`, `s3`, `cloudstorage`, and `blob`.

## Checks

`make commit-check` runs `fmt`, `race`, `test-fuzz`, and `code-ql-gate`. `fmt` writes Go files that `gofmt` would change. `code-ql` runs the CodeQL CLI and writes SARIF under `.tmp/codeql/`. The gate fails when a result is level `error`, ignoring paths under `vendor`. `jq` is required. GitHub Actions runs the same race and fuzz steps, and CodeQL on pull requests.

## Build

```bash
go test ./...
go build -o redisstream ./redisstream
```
