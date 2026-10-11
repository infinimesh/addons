# Snowflake

Writes device events from Redis into Snowflake database `infinimesh`, table `device_states`.

The stream is produced by [addon-redis-stream](https://github.com/infinimesh/addon-redis-stream). Set `REDIS_ADDR` and the `SNOWFLAKE_*` variables. `SNOWFLAKE_INITDB=true` creates the database and table.

Apache-2.0.
