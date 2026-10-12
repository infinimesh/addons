# sdk

Packages under `github.com/infinimesh/addons/sdk`.

- `api`: login, device list and device state stream over the core v3 gRPC API
- `wrappers`: starts and stops one worker per device
- `runner`: the main loop for addons that stream device shadows. It logs in, follows the device list and calls your function for each shadow update.
- `message`: the JSON form of a shadow update, shared by `kafka` and `webhook`
- `config`: environment settings shared by all addons
- `stream/consumer`: reads the Redis stream written by `redisstream`

Apache-2.0.
