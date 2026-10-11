# Redis stream

Reads the Infinimesh object API and publishes device events on a Redis stream. Sink addons consume that stream through [addon-sdk](https://github.com/infinimesh/addon-sdk) `stream/consumer`.

Set `USERNAME`, `PASSWORD`, `API_URL`, and `REDIS_ADDR`.

Apache-2.0.
