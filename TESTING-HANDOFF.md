# Testing handoff: infinimesh/addons against core v3.4.0

Written 2026-10-14. Covers addons main after #31, #32, #33, #30 and #34.

## 0. Unit checks (no services needed)

```
go build ./... && go vet ./... && go test -race ./...
make commit-check   # fmt, race, fuzz, CodeQL gate (needs the codeql CLI)
```

Each addon on its own: `go build -o /dev/null ./<addon>/... && go vet ./<addon>/... && go test -race ./<addon>/...`

## 1. Core v3.4.0 setup

1. Start core v3.4.0 with its compose file (infinimesh/core tag v3.4.0). Note the API URL (for example `http://localhost:8000`, gRPC and Connect on the same port).
2. With `inf` (inf-cli): create an account for the addon (USERNAME, PASSWORD), a namespace, give the account read access to it, and create two devices d1 and d2 in it.
3. Send reported state, for example `curl -X POST $API/devices/states -H "Authorization: Bearer <device token>" -d '{"device":"<d1>","reported":{"data":{"temp":21.5}}}'`.

Common env for every shadow addon:

| var | value |
|---|---|
| `API_URL` | core API URL |
| `USERNAME`, `PASSWORD` | the addon account |
| `TOKEN_REFRESH_INTERVAL_SECS` | default 3600 |
| `DEVICE_REFRESH_INTERVAL_SECS` | how often namespaces are listed again (default 600; use 10 for tests) |

Build and run any addon with `docker build -f <addon>/Dockerfile -t addon-<name> .` from the repo root, or `go run ./<addon>`.

## 2. What to check for every shadow addon

The shadow addons are postgres, influxdb, webhook, mqttbridge, kafka, redisstream, timeseries and cloudconnect csvwriter.

1. Startup log shows one stream per namespace, not one per device. On core, one StreamShadow per namespace (core logs or metrics).
2. A state update for d1 arrives in the sink within a second or two.
3. Add device d3 to the namespace while the addon runs. Log: `device <d3> added to namespace <ns>`. Its next state reaches the sink. No restart.
4. Remove d1 (or move it out of the namespace). Log: `device <d1> removed ...`. Later updates for d1 do not reach the sink.
5. Resume: restart core's API pod (or drop the connection) while sending updates every second. After reconnect, the sink has no gap in the sequence (each shadow carries `sequence`; resume_after is the last one seen).
6. Stop the addon (SIGTERM / Ctrl-C) and check core has no stream left open for it.

## 3. Per addon

| addon | extra env | sink check |
|---|---|---|
| postgres | `POSTGRES_URL`, optional `POSTGRES_TABLE`, `POSTGRES_TIMESCALE` | rows in the table per update |
| influxdb | `INFLUX_URL`, `INFLUX_TOKEN`, `INFLUX_ORG`, `INFLUX_BUCKET` | points in the bucket |
| webhook | `WEBHOOK_URL`, optional `WEBHOOK_SECRET` | POSTs at the target (use a request bin); signature header when a secret is set |
| mqttbridge | `MQTT_BROKER`, `MQTT_TOPIC` | `mosquitto_sub -t '#'` shows messages |
| kafka | `KAFKA_BROKERS`, `KAFKA_TOPIC`, optional `KAFKA_CREATE_TOPIC=true` | `kcat -C -t <topic>`: key is the device uid |
| redisstream | `REDIS_ADDR` | `XRANGE objects - +` shows `object` and `state` JSON; only reported state |
| timeseries | `REDIS_ADDR` (Redis with the RedisTimeSeries module) | `TS.RANGE temp:<uid> - +`; labels uid, name, kind |
| cloudconnect csvwriter | `WRITE_DIR` | `<WRITE_DIR>/<uid>/<unix>.csv`, one row per update |
| cloudconnect s3 / cloudstorage / blob | `READ_DIR` (= csvwriter WRITE_DIR) plus `AWS_S3BUCKETNAME` / `GCP_BUCKET_NAME`, `GCP_PROJECT_ID` / `AZ_ACCOUNT_NAME`, `AZ_ACCOUNT_KEY`, `AZ_CONTAINER_NAME` | files older than a minute are uploaded as `<uid>/<unix>.csv` and then deleted locally; on upload error the file stays |
| elasticsearch | `REDIS_ADDR` (reads the redisstream stream), `ES_ADDR` | documents in the index |
| snowflake | `REDIS_ADDR`, `SNOWFLAKE_ACCOUNT`, `SNOWFLAKE_USER`, `SNOWFLAKE_PASSWORD`, `SNOWFLAKE_WAREHOUSE`, `SNOWFLAKE_ROLE`, `SNOWFLAKE_SCHEMA`, `SNOWFLAKE_INITDB=true` once | rows in the devices table |
| saphana | `REDIS_ADDR`, `SAPHANA_HOST`, `SAPHANA_PORT`, `SAPHANA_USERNAME`, `SAPHANA_PASSWORD`, `SAPHANA_TLS`, `SAPHANA_INITDB=true` once | rows in DEVICES; a bad timestamp is skipped, not retried |

elasticsearch, snowflake and saphana read from the Redis stream that redisstream writes, so run redisstream first. Check that an entry is acknowledged only after a good write (stop the database, send an update, start it again: the row arrives).

## 4. Not covered by unit tests

- Real gRPC against core (all tests use fakes).
- Docker image builds (no Docker daemon on the box; `CGO_ENABLED=0 go build` of every Dockerfile target passes).
- Cloud services (S3, GCS, Azure, Snowflake, SAP HANA): only the mapping and upload calls are tested with fakes.
