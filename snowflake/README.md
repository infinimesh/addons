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
| `REDIS_PASSWORD` | | Redis password |
| `REDIS_TLS` | `false` | connect to Redis with TLS |
| `REDIS_TLS_INSECURE` | `false` | skip the Redis certificate check, for test setups only |
| `REDIS_STREAM_START_ID` | `$` | where the consumer group starts when it is first created. `$` reads only new entries, `0` reads the whole stream. |
| `SNOWFLAKE_ACCOUNT` | | Snowflake account |
| `SNOWFLAKE_USER`, `SNOWFLAKE_PASSWORD` | | Snowflake login |
| `SNOWFLAKE_WAREHOUSE` | user default | warehouse for the inserts |
| `SNOWFLAKE_ROLE` | user default | role to use |
| `SNOWFLAKE_SCHEMA` | user default, usually `PUBLIC` | schema in the `infinimesh` database |
| `SNOWFLAKE_INITDB` | `false` | create the database, the schema and the table on start, if they do not exist |

## Limits

- All values go in as bind parameters, and the data goes through `PARSE_JSON(?)`. Device data cannot change the SQL.
- It reads as consumer group `group`, with the host name as consumer name. By default the group starts at the end of the stream, so entries written before the first start are skipped. Set `REDIS_STREAM_START_ID=0` to read them too.
- An entry is acknowledged only after the write works. A failed entry stays pending and is tried again about once a second, before new entries. Entries that cannot be read, or have a bad timestamp, are dropped with a log line.
- If the host name changes, for example a new container, entries left pending by the old name are not picked up.
- `version` is always empty, because the core v3 shadow has no version.
- Writes to Snowflake are not tested live. Reading the stream, the retry of a failed write and the Redis password were tested against Redis 7.4.

## Run

```bash
docker compose -f snowflake/docker-compose.yml up --build
```

The compose file starts `redisstream`, Redis and this addon, with `SNOWFLAKE_INITDB=true`. Set the `SNOWFLAKE_*` values, and `USERNAME`, `PASSWORD` and `API_URL` for `redisstream`, first.

Apache-2.0.
