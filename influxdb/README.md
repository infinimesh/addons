# InfluxDB

Streams device shadows from the infinimesh API to InfluxDB with the official [influxdb-client-go](https://github.com/influxdata/influxdb-client-go) v2 client. Works with InfluxDB 2, InfluxDB Cloud and InfluxDB 3, which accepts the v2 write API.

Each shadow update becomes one point in line protocol:

```
device_state,device=f3913526-38d9-4bf2-a60d-b616d6ec8dcc,title=pump-7 temp=20,hum=40,gps.lat=35.9,connected=true 1791792000000000000
```

- measurement: `INFLUX_MEASUREMENT`
- tags: `device` (the device id) and `title` (the device name)
- fields: the reported state. Nested objects become `parent.child`. Numbers are floats, so a field never changes type. Lists and nulls are stored as JSON strings. `connected` is added when the update has connection state.
- with `INFLUX_INCLUDE_DESIRED=true` the desired state is added as `desired.<name>` fields
- time: the reported timestamp, or desired, or connection, or the time the addon got it

Updates with no fields, such as a desired only update with desired off, are skipped.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `USERNAME`, `PASSWORD` | | infinimesh account |
| `API_URL` | | infinimesh gRPC address, see the main README |
| `TOKEN_REFRESH_INTERVAL_SECS` | `3600` | how often to log in again |
| `DEVICE_REFRESH_INTERVAL_SECS` | `600` | how often to look for new or removed devices. The older name `OBJECT_REFRESH_INTERVAL_SECS` also works. |
| `INFLUX_URL` | | for example `http://influxdb:8086` |
| `INFLUX_TOKEN` | | API token with write access to the bucket |
| `INFLUX_ORG` | | organization. InfluxDB 3 ignores it. |
| `INFLUX_BUCKET` | | bucket, or the database name on InfluxDB 3 |
| `INFLUX_MEASUREMENT` | `device_state` | measurement name |
| `INFLUX_INCLUDE_DESIRED` | `false` | also write desired state |
| `INFLUX_BATCH_SIZE` | `1000` | most points in one write |
| `INFLUX_FLUSH_INTERVAL_SECS` | `1` | longest wait before queued points are written |
| `INFLUX_MAX_BUFFER` | `100000` | most points kept in memory while InfluxDB is down |
| `INFLUX_TIMEOUT_SECS` | `20` | timeout for one write |
| `INFLUX_TLS_INSECURE` | `false` | skip certificate checks, for test servers only |

## InfluxDB 3

Set `INFLUX_URL` to the InfluxDB 3 address, `INFLUX_BUCKET` to the database and `INFLUX_TOKEN` to a database token. Leave `INFLUX_ORG` empty. InfluxDB 3 Core creates the database on the first write. Query it with SQL:

```sql
SELECT time, device, temp FROM device_state ORDER BY time DESC LIMIT 10;
```

## Limits

- If a write fails the points stay queued and go out with the next flush. When the queue passes `INFLUX_MAX_BUFFER` the oldest points are dropped and logged. Nothing is kept on disk.
- Network errors, timeouts, 401, 403, 408, 429 and 5xx keep the batch queued. Other 4xx answers mean InfluxDB refused the data, so the batch is logged and dropped.
- A field must keep one type. If a device sends `temp` as a number and later as a string, InfluxDB refuses the later points, and the whole batch they are in is dropped. Keep payload types stable.
- `title` is a tag, so renaming a device starts a new series.

## Run

```bash
docker compose -f influxdb/docker-compose.yml up --build
```

The compose file starts InfluxDB 2 next to the addon. Set `USERNAME`, `PASSWORD`, `API_URL` and the tokens first.

Apache-2.0.
