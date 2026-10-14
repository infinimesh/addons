# Kafka

Streams device shadows from the infinimesh API to a Kafka topic. Works with Apache Kafka and with [KafScale](https://kafscale.io) ([KafScale/platform](https://github.com/KafScale/platform)), the Kafka compatible broker that keeps its log in S3.

Each shadow update becomes one record:

- key: the device id, so all updates of one device land in the same partition and stay in order
- value: JSON

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

Parts that are not in the update are left out.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `USERNAME`, `PASSWORD` | | infinimesh account |
| `API_URL` | | infinimesh gRPC address, see the main README |
| `TOKEN_REFRESH_INTERVAL_SECS` | `3600` | how often to log in again |
| `DEVICE_REFRESH_INTERVAL_SECS` | `600` | how often to look for new or removed namespaces. Devices added to or removed from a namespace are picked up at once from the stream. The older name `OBJECT_REFRESH_INTERVAL_SECS` also works. |
| `KAFKA_BROKERS` | | comma separated `host:port` list |
| `KAFKA_TOPIC` | `infinimesh.shadows` | target topic |
| `KAFKA_CLIENT_ID` | `infinimesh-kafka-addon` | Kafka client id. KafScale ACLs use it as the identity. |
| `KAFKA_CREATE_TOPIC` | `true` | create the topic on start. An existing topic is fine. |
| `KAFKA_PARTITIONS`, `KAFKA_REPLICATION_FACTOR` | `1`, `1` | used when the topic is created |
| `KAFKA_ONLY_REPORTED` | `false` | send only reported state, no desired or connection state |
| `KAFKA_TLS` | `false` | connect with TLS |
| `KAFKA_TLS_INSECURE` | `false` | skip certificate checks, for test clusters only |
| `KAFKA_SASL_MECHANISM` | | `PLAIN`, `SCRAM-SHA-256` or `SCRAM-SHA-512` |
| `KAFKA_SASL_USERNAME`, `KAFKA_SASL_PASSWORD` | | SASL login |
| `KAFKA_IDEMPOTENT` | `false` | idempotent producer. Only for plain Kafka. |
| `KAFKA_MAX_VERSION` | | cap Kafka request versions at this Kafka version, for example `3.9`. Only for brokers that cannot negotiate newer versions. |

## KafScale

KafScale speaks the Kafka protocol, so the same settings work. Keep these in mind:

- KafScale has no idempotent producer and no transactions. Leave `KAFKA_IDEMPOTENT` off, which is the default.
- KafScale has no SASL yet. Leave `KAFKA_SASL_MECHANISM` empty and put TLS in front of the broker, at the ingress or mesh, if you need it.
- Point `KAFKA_BROKERS` at the KafScale proxy or broker, for example `kafscale-broker:9092`.
- Writes reach S3 in batches, so expect a few hundred milliseconds before a consumer sees a record.

KafScale builds before [#177](https://github.com/KafScale/platform/pull/177) close the connection when a client opens with ApiVersions v5. Update KafScale, or set `KAFKA_MAX_VERSION=3.9` for an older build.

## Run

```bash
docker compose -f kafka/docker-compose.yml up --build
```

The compose file starts a single node Kafka next to the addon. Set `USERNAME`, `PASSWORD` and `API_URL` first.

Apache-2.0.
