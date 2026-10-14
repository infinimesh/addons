# Timeseries

Streams the reported state of every device the account can see from the infinimesh API to RedisTimeSeries. The compose file also starts Grafana with a Redis data source.

Each number in the reported data becomes one point in a series named `<key>:<device uid>`, for example `temp:f3913526-38d9-4bf2-a60d-b616d6ec8dcc`. Each series has the labels `uid`, `name` and `kind`. The point gets the time the addon writes it.

```text
TS.RANGE temp:f3913526-38d9-4bf2-a60d-b616d6ec8dcc - +
1) 1) 1791792447457
   2) 20
```

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `USERNAME`, `PASSWORD` | | infinimesh account |
| `API_URL` | | infinimesh gRPC address, see the main README |
| `TOKEN_REFRESH_INTERVAL_SECS` | `3600` | how often to log in again |
| `DEVICE_REFRESH_INTERVAL_SECS` | `600` | how often to look for new or removed namespaces. Devices added to or removed from a namespace are picked up at once from the stream. The older name `OBJECT_REFRESH_INTERVAL_SECS` also works. |
| `REDIS_ADDR` | | Redis `host:port`. Redis needs the RedisTimeSeries module. |
| `REDIS_PASSWORD` | | Redis password |
| `REDIS_TLS` | `false` | connect to Redis with TLS |
| `REDIS_TLS_INSECURE` | `false` | skip the Redis certificate check, for test setups only |

## Limits

- Only top level numbers are stored. Strings, booleans and nested objects are logged and skipped.
- Series are created with no retention, so they keep all points.
- `sample-dashboard.json` is not provisioned. Import it in Grafana by hand.
- Tested live against core: 5 reported states landed as 5 points, with RedisTimeSeries 1.4.7 and with redis-stack-server 7.4.0-v6.

## Run

```bash
docker compose -f timeseries/docker-compose.yml up --build
```

The compose file starts redis-stack-server, Grafana on port 3000 with the Redis data source, and this addon. Set `USERNAME`, `PASSWORD` and `API_URL` first.

Apache-2.0.
