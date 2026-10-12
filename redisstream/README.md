# Redis stream

Streams the reported state of every device the account can see from the infinimesh API to a Redis stream. The sinks in this repository ([`elasticsearch`](../elasticsearch/README.md), [`snowflake`](../snowflake/README.md), [`saphana`](../saphana/README.md)) read that stream.

Each reported state update becomes one entry on the stream `objects`, with two fields:

- `object`: the device, as JSON
- `state`: the reported state, as JSON

```json
object: {"uid": "f3913526-38d9-4bf2-a60d-b616d6ec8dcc", "name": "pump-7", "kind": "device"}
state:  {"version": "", "timestamp": "2026-10-12T08:19:51.617Z", "data": {"temp": 20, "hum": 40}}
```

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `USERNAME`, `PASSWORD` | | infinimesh account |
| `API_URL` | | infinimesh gRPC address, see the main README |
| `TOKEN_REFRESH_INTERVAL_SECS` | `3600` | how often to log in again |
| `DEVICE_REFRESH_INTERVAL_SECS` | `600` | how often to look for new or removed devices. The older name `OBJECT_REFRESH_INTERVAL_SECS` also works. |
| `REDIS_ADDR` | | Redis `host:port` |
| `REDIS_PASSWORD` | | Redis password |
| `REDIS_TLS` | `false` | connect to Redis with TLS |
| `REDIS_TLS_INSECURE` | `false` | skip the Redis certificate check, for test setups only |
| `REDIS_STREAM_MAXLEN` | `100000` | trim the stream to about this many entries. `0` turns trimming off. |

## Limits

- Only reported state is sent. Desired and connection state are not.
- Trimming is approximate (`MAXLEN ~`), so the stream can be a bit longer than `REDIS_STREAM_MAXLEN`. Entries that sinks have not read yet can be trimmed away if they fall far behind.
- `version` is always empty, because the core v3 shadow has no version.
- Not tested live. It uses the same SDK calls as `timeseries`, which was tested against core.

## Run

```bash
docker compose -f redisstream/docker-compose.yml up --build
```

The compose file starts Redis next to the addon. Set `USERNAME`, `PASSWORD` and `API_URL` first.

Apache-2.0.
