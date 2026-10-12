# SAP HANA

Writes device state from Redis into the SAP HANA table `devices`. It reads the Redis stream `objects` that [`redisstream`](../redisstream/README.md) writes. Run `redisstream` next to it.

Each stream entry becomes one row:

| Column | Type | Value |
|---|---|---|
| `uid` | `NVARCHAR(1000)` | device id |
| `timestamp` | `TIMESTAMP` | time of the reported state |
| `version` | `NVARCHAR(1000)` | always empty |
| `data` | `NCLOB` | reported data as a JSON string |

The primary key is `(uid, timestamp)`. The data is a JSON string and not a HANA JSON document, because SAP HANA Cloud did not offer the JSON document store when this addon was written.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `REDIS_ADDR` | | Redis `host:port` |
| `REDIS_PASSWORD` | | Redis password |
| `REDIS_TLS` | `false` | connect to Redis with TLS |
| `REDIS_TLS_INSECURE` | `false` | skip the Redis certificate check, for test setups only |
| `REDIS_STREAM_START_ID` | `$` | where the consumer group starts when it is first created. `$` reads only new entries, `0` reads the whole stream. |
| `SAPHANA_HOST`, `SAPHANA_PORT` | | SAP HANA address. The compose file uses port `443`. |
| `SAPHANA_USERNAME`, `SAPHANA_PASSWORD` | | SAP HANA login |
| `SAPHANA_TLS` | `true` | connect with TLS and check the certificate against `SAPHANA_HOST` |
| `SAPHANA_INITDB` | `false` | create the `devices` table on start, if it does not exist in the current schema |

## Limits

- A `devices` table made by an older version has `data varchar(5000)`. `SAPHANA_INITDB` does not change an existing table, so move it to `NCLOB` yourself if you need larger data.
- Turn `SAPHANA_TLS` off only on a trusted network. SAP HANA Cloud needs TLS.
- It reads as consumer group `group`, with the host name as consumer name. By default the group starts at the end of the stream, so entries written before the first start are skipped. Set `REDIS_STREAM_START_ID=0` to read them too.
- An entry is acknowledged only after the write works. A failed entry stays pending and is tried again about once a second, before new entries. Entries that cannot be read, or have a bad timestamp, are dropped with a log line.
- If the host name changes, for example a new container, entries left pending by the old name are not picked up.
- `version` is always empty, because the core v3 shadow has no version.
- Writes to SAP HANA are not tested live. Reading the stream, the retry of a failed write and the Redis password were tested against Redis 7.4.

## Run

```bash
docker compose -f saphana/docker-compose.yml up --build
```

The compose file starts `redisstream`, Redis and this addon, with `SAPHANA_INITDB=true`. Set the `SAPHANA_*` values, and `USERNAME`, `PASSWORD` and `API_URL` for `redisstream`, first.

Apache-2.0.
