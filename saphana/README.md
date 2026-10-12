# SAP HANA

Writes device state from Redis into the SAP HANA table `devices`. It reads the Redis stream `objects` that [`redisstream`](../redisstream/README.md) writes. Run `redisstream` next to it.

Each stream entry becomes one row:

| Column | Type | Value |
|---|---|---|
| `uid` | `varchar(1000)` | device id |
| `timestamp` | `timestamp` | time of the reported state |
| `version` | `varchar(1000)` | always empty |
| `data` | `varchar(5000)` | reported data as a JSON string |

The primary key is `(uid, timestamp)`. The data is a JSON string and not a HANA JSON document, because SAP HANA Cloud did not offer the JSON document store when this addon was written.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `REDIS_ADDR` | | Redis `host:port` |
| `SAPHANA_HOST`, `SAPHANA_PORT` | | SAP HANA address. The compose file uses port `443`. |
| `SAPHANA_USERNAME`, `SAPHANA_PASSWORD` | | SAP HANA login |
| `SAPHANA_INITDB` | `false` | create the `devices` table on start |

## Limits

- The connection always uses TLS and checks the certificate against `SAPHANA_HOST`. There is no setting for plain connections.
- `SAPHANA_INITDB` does not check if the table exists. On a second start it logs an error and goes on.
- Data longer than 5000 characters does not fit in the `data` column, and that insert fails.
- It reads as consumer group `group`, with the host name as consumer name. The group is created at the end of the stream, so entries written before the first start are skipped.
- Each entry is acknowledged before it is written. If the write fails, the entry is logged and lost.
- `version` is always empty, because the core v3 shadow has no version.
- Not tested live.

## Run

```bash
docker compose -f saphana/docker-compose.yml up --build
```

The compose file starts `redisstream`, Redis and this addon, with `SAPHANA_INITDB=true`. Set the `SAPHANA_*` values, and `USERNAME`, `PASSWORD` and `API_URL` for `redisstream`, first.

Apache-2.0.
