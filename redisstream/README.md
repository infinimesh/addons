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
| `TOKEN_REFRESH_INTERVAL_SECS` | none, required | how often to log in again |
| `OBJECT_REFRESH_INTERVAL_SECS` | none, required | how often to look for new or removed devices |
| `REDIS_ADDR` | | Redis `host:port` |

## Limits

- Only reported state is sent. Desired and connection state are not.
- The stream is never trimmed, so it grows until you trim it yourself, for example with `XTRIM objects MAXLEN ~ 100000`.
- The Redis connection has no password or TLS setting.
- `version` is always empty, because the core v3 shadow has no version.
- Not tested live. It uses the same SDK calls as `timeseries`, which was tested against core.

## Run

```bash
docker compose -f redisstream/docker-compose.yml up --build
```

The compose file starts Redis next to the addon. Set `USERNAME`, `PASSWORD` and `API_URL` first.

Apache-2.0.
