# Snowflake

Writes device state from Redis into the Snowflake database `infinimesh`, table `device_states`. It reads the Redis stream `objects` that [`redisstream`](../redisstream/README.md) writes. Run `redisstream` next to it.

Each stream entry becomes one row:

| Column | Type | Value |
|---|---|---|
| `uid` | `VARCHAR(1000)` | device id |
| `timestamp` | `TIMESTAMP` | time of the reported state |
| `version` | `VARCHAR(1000)` | always empty |
| `data` | `VARIANT` | reported data as JSON |

The primary key is `(uid, timestamp)`.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `REDIS_ADDR` | | Redis `host:port` |
| `SNOWFLAKE_ACCOUNT` | | Snowflake account |
| `SNOWFLAKE_USER`, `SNOWFLAKE_PASSWORD` | | Snowflake login |
| `SNOWFLAKE_INITDB` | `false` | create the database and the table on start, if they do not exist |

## Limits

- There are no settings for warehouse, role or schema. The user's defaults are used, so the user needs a default warehouse.
- The data JSON is put straight into the SQL text inside `PARSE_JSON('...')`. A single quote in device data breaks the insert. Only connect devices you trust until this is fixed.
- It reads as consumer group `group`, with the host name as consumer name. The group is created at the end of the stream, so entries written before the first start are skipped.
- Each entry is acknowledged before it is written. If the write fails, the entry is logged and lost.
- `version` is always empty, because the core v3 shadow has no version.
- Not tested live.

## Run

```bash
docker compose -f snowflake/docker-compose.yml up --build
```

The compose file starts `redisstream`, Redis and this addon, with `SNOWFLAKE_INITDB=true`. Set the `SNOWFLAKE_*` values, and `USERNAME`, `PASSWORD` and `API_URL` for `redisstream`, first.

Apache-2.0.
