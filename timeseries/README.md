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
| `TOKEN_REFRESH_INTERVAL_SECS` | none, required | how often to log in again |
| `DEVICE_REFRESH_INTERVAL_SECS` | none, required | how often to look for new or removed devices |
| `REDIS_ADDR` | | Redis `host:port`. Redis needs the RedisTimeSeries module. |

## Limits

- Only top level numbers are stored. Strings, booleans and nested objects are logged and skipped.
- Series are created with no retention, so they keep all points.
- The Redis connection has no password or TLS setting.
- `sample-dashboard.json` is not provisioned. Import it in Grafana by hand.
- Tested live against core: 5 reported states landed as 5 points, with RedisTimeSeries 1.4.7 and with redis-stack-server 7.4.0-v6.

## Run

```bash
docker compose -f timeseries/docker-compose.yml up --build
```

The compose file starts redis-stack-server, Grafana on port 3000 with the Redis data source, and this addon. Set `USERNAME`, `PASSWORD` and `API_URL` first.

Apache-2.0.
