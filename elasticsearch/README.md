# Elasticsearch

Writes device state from Redis into the Elasticsearch index `infinimesh`. It reads the Redis stream `objects` that [`redisstream`](../redisstream/README.md) writes. Run `redisstream` next to it.

Each stream entry becomes one document. The document id is `<device uid>-<state timestamp in unix nanoseconds>`. The document body is the device and its reported state:

```json
{"uid": "f3913526-38d9-4bf2-a60d-b616d6ec8dcc", "name": "pump-7", "kind": "device", "version": "", "timestamp": "2026-10-12T08:19:51.617Z", "data": {"temp": 20, "hum": 40}}
```

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `REDIS_ADDR` | | Redis `host:port` |
| `REDIS_PASSWORD` | | Redis password |
| `REDIS_TLS` | `false` | connect to Redis with TLS |
| `REDIS_TLS_INSECURE` | `false` | skip the Redis certificate check, for test setups only |
| `REDIS_STREAM_START_ID` | `$` | where the consumer group starts when it is first created. `$` reads only new entries, `0` reads the whole stream. |
| `ES_ADDR` | | Elasticsearch URL, for example `http://elasticsearch:9200` |

## Limits

- The same state read twice gets the same id, so a retry does not make a second document.
- There are no settings for Elasticsearch login or TLS beyond what fits in `ES_ADDR`.
- The client is the Elasticsearch v7 Go client. The compose file uses Elasticsearch 7.17.29.
- It reads as consumer group `group`, with the host name as consumer name. By default the group starts at the end of the stream, so entries written before the first start are skipped. Set `REDIS_STREAM_START_ID=0` to read them too.
- An entry is acknowledged only after the write works. A failed entry stays pending and is tried again about once a second, before new entries. Entries that cannot be read, or have a bad timestamp, are dropped with a log line.
- If the host name changes, for example a new container, entries left pending by the old name are not picked up.
- `version` is always empty, because the core v3 shadow has no version.
- Writes to Elasticsearch are not tested live. Reading the stream, the retry of a failed write and the Redis password were tested against Redis 7.4.

## Run

```bash
docker compose -f elasticsearch/docker-compose.yml up --build
```

The compose file starts `redisstream`, Redis, Elasticsearch and this addon. Set `USERNAME`, `PASSWORD` and `API_URL` for `redisstream` first.

Apache-2.0.
