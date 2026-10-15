# hawkBit

Tested on core v3.4.1.

Connects one infinimesh namespace to a hawkBit update server. The device uuid is the hawkBit controller id. A deployment started in hawkBit is written to `desired.ota.version`. `inf ota assign` stores `requested_version`, and this addon assigns the distribution set of that name.

You need Docker, a core v3.4.1 API, and the `inf` CLI.

## Register

Log in as root, create the plugin, and turn OTA on for the namespace. The plugin bearer is printed once.

```bash
inf login --root
inf plugins create --kind ota --provider hawkbit --title hawkbit --namespace <namespace>
inf namespaces update <namespace> --ota --provider hawkbit
```

`ota cannot be enabled until an OTA addon is registered` means the plugin does not exist yet. Create it first.

`ota provider requires the addon token` means the call used an account token. `desired.ota.version` is written by this process with `OTA_ADDON_TOKEN`.

`ota provider does not match the namespace` means the namespace provider is not `hawkbit`, or this token belongs to another provider. The process logs that once and leaves the namespace alone.

`token is revoked` means the plugin was rotated, its provider changed, or it was deleted. The process logs `token is revoked; run inf plugins rotate and update the secret` and exits non-zero, including when `OTA_POLL_SECS` is `0`. Run `inf plugins rotate <plugin>`, put the new bearer in `OTA_ADDON_TOKEN`, and start again.

If hawkBit does not answer, the process writes `reported.ota.error` `hawkbit unreachable` and keeps running. It tries the same device again on the next event or poll. It does not post the action twice. A target with no distribution set is not that error, and a later successful read clears `hawkbit unreachable`.

hawkBit 0.9.0 reports `detailStatus` in lowercase (`running`, `retrieved`, `finished`). That is what is written to `reported.ota.status`. An `ota:request` that arrives while this process is down is deployed after the next start.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `OTA_ADDON_TOKEN` | | plugin bearer |
| `OTA_NAMESPACE` | | one namespace. Use this or `OTA_NAMESPACES`, not both |
| `OTA_NAMESPACES` | | comma separated namespaces for a public plugin |
| `API_URL` | | core gRPC address. `https://api.example` or, with `OTA_ALLOW_INSECURE=1`, `http://api:8000` |
| `HAWKBIT_URL` | | hawkBit Management API, for example `http://hawkbit:8080` |
| `HAWKBIT_USER`, `HAWKBIT_PASSWORD` | | Management API login. The image in this folder uses `admin` / `admin` |
| `OTA_ALLOW_INSECURE` | | set to `1` to allow an `http://` `API_URL` or `HAWKBIT_URL`. Without it the process stops with `http:// refused without OTA_ALLOW_INSECURE` |
| `OTA_GROUP_TAGS` | | device tags copied to hawkBit as `tag-<name>`. Empty keeps only `ns-<namespace>` |
| `OTA_POLL_SECS` | `15` | seconds between reads of hawkBit. `0` reads only when core sends an event |
| `OTA_STATE_FILE` | | file of device ids and deploy keys. A restart reads it and does not post the same action again |

hawkBit image: `hawkbit/hawkbit-update-server:0.9.0`. The status table in this addon matches that build.

## Run

```bash
docker compose -f ota-hawkbit/docker-compose.yml up --build
```

Set `OTA_ADDON_TOKEN`, `OTA_NAMESPACE`, and `API_URL` in that file first. The compose file starts hawkBit beside the addon so you can try the Management API. Point `HAWKBIT_URL` at your own server and remove the `hawkbit` service when you have one.

Assign a version from an account login:

```bash
inf ota assign --namespace <namespace> --device <device> --version <distribution set name>
```

The distribution set must already exist in hawkBit. A missing name is written once as `reported.ota.status` `failed` and is not retried.

Apache-2.0.
