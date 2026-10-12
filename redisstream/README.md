# Redis stream

Streams device state from the infinimesh API and publishes device events on a Redis stream. Sinks consume that stream through `sdk/stream/consumer`.

Set `USERNAME`, `PASSWORD`, `API_URL`, and `REDIS_ADDR`.

Apache-2.0.
