# MQTT bridge

Streams device shadows from the infinimesh API to an MQTT broker you already run, such as Mosquitto, EMQX, HiveMQ or AWS IoT Core. It uses [Eclipse Paho](https://github.com/eclipse/paho.mqtt.golang) and MQTT 3.1.1.

Each shadow update becomes one message on the topic from `MQTT_TOPIC`, with a JSON payload:

```json
{
  "device": "f3913526-38d9-4bf2-a60d-b616d6ec8dcc",
  "title": "pump-7",
  "reported": {"timestamp": "2026-10-12T08:19:51.617Z", "data": {"temp": 20, "hum": 40}},
  "desired": {"timestamp": "2026-10-12T08:10:02.100Z", "data": {"fan": "on"}},
  "connection": {"connected": true, "timestamp": "2026-10-12T08:00:00Z", "client_id": "pump-7"},
  "sent_at": "2026-10-12T08:19:51.618Z"
}
```

Parts that are not in the update are left out. This is the same format the Kafka and webhook addons send.

The topic template can use `{device}` (the device id) and `{title}` (the device name). `/`, `+` and `#` in a value become `_`, so a value stays one topic level. For example `site/{title}/{device}` gives `site/pump-7/f3913526-38d9-4bf2-a60d-b616d6ec8dcc`.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `USERNAME`, `PASSWORD` | | infinimesh account |
| `API_URL` | | infinimesh gRPC address, see the main README |
| `TOKEN_REFRESH_INTERVAL_SECS` | `3600` | how often to log in again |
| `DEVICE_REFRESH_INTERVAL_SECS` | `600` | how often to look for new or removed namespaces. Devices added to or removed from a namespace are picked up at once from the stream. The older name `OBJECT_REFRESH_INTERVAL_SECS` also works. |
| `MQTT_BROKER` | | `tcp://host:1883`, `ssl://host:8883`, `ws://host/mqtt` or `wss://host/mqtt` |
| `MQTT_CLIENT_ID` | `infinimesh-mqttbridge` | MQTT client id. Use a different one for each running copy. |
| `MQTT_USERNAME`, `MQTT_PASSWORD` | | broker login |
| `MQTT_TOPIC` | `infinimesh/devices/{device}/shadow` | topic template |
| `MQTT_QOS` | `1` | `0`, `1` or `2` |
| `MQTT_RETAIN` | `false` | publish as retained, so new subscribers get the last state of each device |
| `MQTT_TIMEOUT_SECS` | `10` | connect, write and publish timeout |
| `MQTT_ONLY_REPORTED` | `false` | send only reported state, no desired or connection state |
| `MQTT_TLS_CA_FILE` | | PEM file with the CA that signed the broker certificate. Empty means the system CAs. |
| `MQTT_TLS_CERT_FILE`, `MQTT_TLS_KEY_FILE` | | PEM client certificate and key, for brokers that use certificate login |
| `MQTT_TLS_INSECURE` | `false` | skip certificate checks, for test brokers only |

TLS is used for `ssl://`, `tls://`, `mqtts://` and `wss://` brokers. Mount the PEM files into the container, for example with a compose `volumes` entry.

## Limits

- The bridge only publishes. It does not subscribe, so it cannot change desired state from MQTT.
- If the broker is down, paho reconnects by itself and updates in that time fail and are logged. There is no disk queue.
- MQTT 5 is not supported yet. Most brokers accept 3.1.1 clients.

## Run

```bash
docker compose -f mqttbridge/docker-compose.yml up --build
```

The compose file starts Mosquitto without auth next to the addon. Set `USERNAME`, `PASSWORD` and `API_URL` first.

Apache-2.0.
