# Rules

Checks every device shadow update against rules in a YAML file and raises an alarm when a rule matches. An alarm can fire a webhook, an MQTT message, a desired state change and a log line. Alarms are kept per rule and device, with three states: `raised`, `acked` and `cleared`.

A rules file:

```yaml
rules:
  - name: pump-hot
    when: '"pump" in tags && reported.temp > 80'
    clear: reported.temp < 75
    cooldown: 10m
    repeat: 1h
    on_clear: true
    actions:
      - log: {message: "pump too hot"}
      - webhook: {url: "https://hooks.example.com/alarms", secret: "${RULES_WEBHOOK_SECRET}"}
      - mqtt: {topic: "infinimesh/alarms/{rule}/{device}", qos: 1}
      - desired: {fan: "on"}
```

`${VAR}` is replaced with the environment variable, so secrets stay out of the file. See [`rules.example.yaml`](rules.example.yaml).

Rule fields:

| Field | Meaning |
|---|---|
| `name` | unique name, no spaces or `/` |
| `when` | [CEL](https://cel.dev) condition that raises the alarm |
| `clear` | CEL condition that clears it. Without it, the alarm clears when `when` is false again. |
| `cooldown` | shortest time between two raises of the same rule and device, such as `10m` |
| `repeat` | fire the actions again this often while the alarm stays raised and nobody acked it |
| `on_clear` | also run the actions when the alarm clears. `desired` is never set on clear. |
| `actions` | list of `log`, `webhook`, `mqtt` and `desired`. Default is one `log`. |

A condition can use:

| Name | Type | Meaning |
|---|---|---|
| `reported` | map | last reported data, such as `reported.temp` |
| `desired` | map | last desired data |
| `connected` | bool | whether the device is connected |
| `device`, `title` | string | device id and title |
| `tags` | list | device tags, such as `"pump" in tags` |
| `ns` | string | name of the namespace the device is in |

An update can carry only part of the shadow. The addon keeps the last reported, desired and connection state of each device, so every condition sees the full picture. If a condition reads a field that is missing, such as `reported.temp` before the first reading, the alarm is left as it is. Numbers of different types compare as you would expect, so `reported.temp > 80` works for `80.5`.

Webhook and MQTT actions send this JSON:

```json
{
  "event": "raised",
  "rule": "pump-hot",
  "device": "f3913526-38d9-4bf2-a60d-b616d6ec8dcc",
  "title": "pump-7",
  "namespace": "infinimesh",
  "tags": ["pump"],
  "alarm": {"rule": "pump-hot", "device": "f3913526-38d9-4bf2-a60d-b616d6ec8dcc", "state": "raised", "raised_at": "2026-10-12T08:19:51Z", "fired_at": "2026-10-12T08:19:51Z", "count": 1},
  "shadow": {"device": "f3913526-38d9-4bf2-a60d-b616d6ec8dcc", "reported": {"data": {"temp": 85}}, "sent_at": "2026-10-12T08:19:51Z"},
  "sent_at": "2026-10-12T08:19:51Z"
}
```

`event` is `raised`, `repeat` or `cleared`. `shadow` is in the same format the other addons send. The webhook signs the body like the webhook addon does, with `X-Infinimesh-Signature: sha256=<hex>` when `secret` is set. MQTT topics can use `{rule}`, `{device}` and `{event}`.

The alarm API:

```bash
curl -H "Authorization: Bearer $RULES_API_TOKEN" http://localhost:8080/alarms?state=raised
curl -X POST -H "Authorization: Bearer $RULES_API_TOKEN" http://localhost:8080/alarms/pump-hot/<device>/ack
```

An ack stops repeats. The alarm clears as usual when the device is fine again. `GET /healthz` needs no token.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `USERNAME`, `PASSWORD` | | infinimesh account. It needs MGMT access to devices for `desired` actions. |
| `API_URL` | | infinimesh gRPC address, see the main README |
| `TOKEN_REFRESH_INTERVAL_SECS` | `3600` | how often to log in again |
| `DEVICE_REFRESH_INTERVAL_SECS` | `600` | how often to look for new or removed devices |
| `RULES_FILE` | `/etc/infinimesh/rules.yaml` | the rules file |
| `RULES_STATE_FILE` | `/var/lib/infinimesh-rules/alarms.json` | where alarms are kept between restarts |
| `RULES_HTTP_ADDR` | `:8080` | address of the alarm API. `off` turns it off. |
| `RULES_API_TOKEN` | | bearer token for the alarm API. Required unless the API is off. |
| `RULES_MQTT_BROKER` | | `tcp://`, `ssl://`, `ws://` or `wss://` broker URL for `mqtt` actions |
| `RULES_MQTT_USERNAME`, `RULES_MQTT_PASSWORD` | | broker login |
| `RULES_MQTT_CLIENT_ID` | `infinimesh-rules` | MQTT client id |
| `RULES_WEBHOOK_TIMEOUT_SECS` | `10` | timeout for one webhook request or MQTT publish |
| `RULES_WEBHOOK_MAX_RETRIES` | `5` | extra webhook tries after the first one fails |
| `RULES_WEBHOOK_BACKOFF_MS` | `500` | wait before the first retry. It doubles after each retry. |
| `RULES_WEBHOOK_MAX_BACKOFF_SECS` | `30` | longest wait between retries |

## Limits

- Alarms are saved to the state file on every change, so a restart does not fire them again. The last known device state is not saved, so after a restart a rule waits for the next update of each device.
- Rules are checked on device updates only. A `repeat` fires on the next update after the repeat time, not on a timer, and a device that stops sending is not caught by a reported value rule. Use `!connected` for that.
- Webhooks retry network errors, timeouts, 408, 429 and 5xx, then the event is dropped and logged. MQTT and `desired` actions are tried once. There is no disk queue.
- Tags and namespace are read when the device list is refreshed. A tag change shows up after a restart of the addon.
- The rules file is read at start. Restart the addon to load changes.
- One addon process per state file. Two copies would fire the same alarms twice.

## Run

```bash
docker compose -f rules/docker-compose.yml up --build
```

Set `USERNAME`, `PASSWORD`, `API_URL`, `RULES_API_TOKEN` and `RULES_WEBHOOK_SECRET` first, and edit `rules/rules.example.yaml`.

Apache-2.0.
