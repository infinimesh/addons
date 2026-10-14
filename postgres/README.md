# Postgres

Streams device shadows from the infinimesh API into a Postgres table. Works with plain Postgres and, if you turn it on, makes the table a [TimescaleDB](https://github.com/timescale/timescaledb) hypertable.

Each shadow update becomes one row:

| Column | Type | Value |
|---|---|---|
| `time` | `timestamptz` | reported timestamp, or desired, or connection, or the time the addon got it |
| `device` | `text` | device id |
| `title` | `text` | device name |
| `reported` | `jsonb` | reported state data, or null |
| `desired` | `jsonb` | desired state data, or null |
| `connected` | `boolean` | connection state, or null |

On start the addon creates the schema, table and an index on `(device, time DESC)` if they are missing. With `POSTGRES_TIMESCALE=true` it also creates the `timescaledb` extension and turns the table into a hypertable on `time`. An existing table is left as it is, so changing the columns is up to you.

Example query:

```sql
SELECT time, (reported->>'temp')::float AS temp
FROM device_shadows
WHERE device = 'f3913526-38d9-4bf2-a60d-b616d6ec8dcc'
ORDER BY time DESC LIMIT 100;
```

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `USERNAME`, `PASSWORD` | | infinimesh account |
| `API_URL` | | infinimesh gRPC address, see the main README |
| `TOKEN_REFRESH_INTERVAL_SECS` | `3600` | how often to log in again |
| `DEVICE_REFRESH_INTERVAL_SECS` | `600` | how often to look for new or removed namespaces. Devices added to or removed from a namespace are picked up at once from the stream. The older name `OBJECT_REFRESH_INTERVAL_SECS` also works. |
| `POSTGRES_URL` | | connection string, for example `postgres://user:pass@host:5432/db?sslmode=require`. All [pgx settings](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool#ParseConfig) work, such as `pool_max_conns`. |
| `POSTGRES_TABLE` | `device_shadows` | `table` or `schema.table` |
| `POSTGRES_TIMESCALE` | `false` | create the extension and a hypertable |
| `POSTGRES_TIMESCALE_CHUNK_DAYS` | `7` | hypertable chunk size, used when the hypertable is created |
| `POSTGRES_BATCH_SIZE` | `500` | most rows in one insert |
| `POSTGRES_FLUSH_INTERVAL_SECS` | `1` | longest wait before queued rows are written |
| `POSTGRES_MAX_BUFFER` | `100000` | most rows kept in memory while the database is down |
| `POSTGRES_ONLY_REPORTED` | `false` | store only reported state, no desired or connection state |

## Limits

- Rows are written with `COPY`, in batches. If a write fails the rows stay queued and go out with the next flush. When the queue passes `POSTGRES_MAX_BUFFER` the oldest rows are dropped and logged. Nothing is kept on disk, so a restart loses queued rows.
- There is no unique key, so a row can be written twice if a write succeeds but the answer is lost.
- The account needs rights to create the table, and to create the extension when `POSTGRES_TIMESCALE=true`. Create them yourself beforehand if it cannot.

## Run

```bash
docker compose -f postgres/docker-compose.yml up --build
```

The compose file starts TimescaleDB next to the addon. Set `USERNAME`, `PASSWORD` and `API_URL` first.

Apache-2.0.
