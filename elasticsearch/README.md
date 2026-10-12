# Elasticsearch

Writes device state from Redis into the Elasticsearch index `infinimesh`. It reads the Redis stream `objects` that [`redisstream`](../redisstream/README.md) writes. Run `redisstream` next to it.

Each stream entry becomes one document. The document id is `<device uid>-<unix seconds of the state timestamp>`. The document body is the reported state:

```json
{"version": "", "timestamp": "2026-10-12T08:19:51.617Z", "data": {"temp": 20, "hum": 40}}
```

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `REDIS_ADDR` | | Redis `host:port` |
| `ES_ADDR` | | Elasticsearch URL, for example `http://elasticsearch:9200` |

## Limits

- The document body has no device id. The id is only in the document id.
- Two updates of one device in the same second get the same id, so the second one replaces the first.
- There are no settings for Elasticsearch login or TLS beyond what fits in `ES_ADDR`.
- The client is the Elasticsearch v7 Go client. The compose file uses Elasticsearch 7.17.29.
- It reads as consumer group `group`, with the host name as consumer name. The group is created at the end of the stream, so entries written before the first start are skipped.
- Each entry is acknowledged before it is written. If the write fails, the entry is logged and lost.
- `version` is always empty, because the core v3 shadow has no version.
- Not tested live.

## Run

```bash
docker compose -f elasticsearch/docker-compose.yml up --build
```

The compose file starts `redisstream`, Redis, Elasticsearch and this addon. Set `USERNAME`, `PASSWORD` and `API_URL` for `redisstream` first.

Apache-2.0.
