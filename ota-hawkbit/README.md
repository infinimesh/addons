# hawkBit

Tested on core v3.4.1.

Connects one infinimesh namespace to a hawkBit update server. The device uuid is the hawkBit controller id. A deployment started in hawkBit is written to `desired.ota.version`. `inf ota assign` stores `requested_version`, and this addon assigns the distribution set of that name.

```json
{
  "desired": {"ota": {"version": "1.2.0"}},
  "reported": {"ota": {"status": "installing", "error": "", "updated_at": "2026-10-15T00:00:00Z"}}
}
```

You need Docker, a core v3.4.1 API, and the `inf` CLI.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `OTA_ADDON_TOKEN` | | plugin bearer |
| `OTA_NAMESPACE` | | one namespace. Use this or `OTA_NAMESPACES`, not both |
| `OTA_NAMESPACES` | | comma separated namespaces for a public plugin |
| `API_URL` | | core gRPC address. `https://api.example` or, with `OTA_ALLOW_INSECURE=1`, `http://api:8000` |
| `HAWKBIT_URL` | | hawkBit Management API, for example `http://hawkbit:8080` |
| `HAWKBIT_USER`, `HAWKBIT_PASSWORD` | | Management API login. Required. The compose file also sets this login on its hawkBit and removes hawkBit's built-in `admin` / `admin` |
| `OTA_ALLOW_INSECURE` | | set to `1` to allow an `http://` `API_URL` or `HAWKBIT_URL`. Without it the process stops with `http:// refused without OTA_ALLOW_INSECURE` |
| `OTA_GROUP_TAGS` | | device tags copied to hawkBit as `tag-<name>`. Empty keeps only `ns-<namespace>` |
| `OTA_POLL_SECS` | `15` | seconds between reads of hawkBit. `0` reads only when core sends an event |
| `OTA_STATE_FILE` | | required. File of device ids and deploy keys. A restart reads it and does not post the same action again |

hawkBit image: `hawkbit/hawkbit-update-server:0.9.0`. The status table in this addon matches that build.

## Limits

If hawkBit does not answer, the process writes `reported.ota.error` `hawkbit unreachable` and keeps running. It tries the same device again on the next event or poll. It does not post the action twice. A target with no distribution set is not that error, and a later successful read clears `hawkbit unreachable`.

When hawkBit hangs instead of refusing connections, the addon can take about 90 s before it writes `reported.ota.error` `hawkbit unreachable`. It stays up during that time and deploys pending requests once hawkBit answers again. A hawkBit request that times out is the same outage as a refused connection.

If hawkBit comes back with no targets, the addon creates them again and deploys the pending version.

hawkBit 0.9.0 reports `detailStatus` in lowercase. `running` means the server is waiting for the device. Core has no pending status, so that is `reported.ota.status` `idle`. `downloading` and `installing` come from the device's feedback messages. `retrieved` with no installing feedback stays `downloading`. An `ota:request` that arrives while this process is down is deployed after the next start.

A missing distribution set name is written once as `reported.ota.status` `failed` and is not retried.

## Run

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

```bash
HAWKBIT_USER=<user> HAWKBIT_PASSWORD=<password> docker compose -f ota-hawkbit/docker-compose.yml up --build
```

Set `OTA_ADDON_TOKEN`, `OTA_NAMESPACE`, and `API_URL` in that file first, and export `HAWKBIT_USER` and `HAWKBIT_PASSWORD`. Without them compose stops with `required variable HAWKBIT_USER is missing a value`. The hawkBit in the compose file listens on 127.0.0.1:8080 only. The compose file starts hawkBit beside the addon so you can try the Management API. Point `HAWKBIT_URL` at your own server and remove the `hawkbit` service when you have one.

Assign a version from an account login:

```bash
inf ota assign --namespace <namespace> --device <device> --version <distribution set name>
```

The distribution set must already exist in hawkBit.

## Let devices log in to hawkBit

Devices report downloading, installing and success to hawkBit, not to this addon. A fresh hawkBit 0.9.0 has target token auth off, so a device that calls the DDI API with `Authorization: TargetToken <token>` gets 401 and `reported.ota.status` stays `idle`. Turn it on with a Management API login:

```bash
curl -u <user>:<password> -H 'Content-Type: application/json' -X PUT http://localhost:8080/rest/v1/system/configs/authentication.targettoken.enabled -d '{"value":true}'
```

Then give each device its token. It is the `securityToken` field of:

```bash
curl -u <user>:<password> http://localhost:8080/rest/v1/targets/<device uuid>
```

The device calls `GET /DEFAULT/controller/v1/<device uuid>` with `Authorization: TargetToken <securityToken>`. Gateway token auth (`authentication.gatewaytoken.enabled`) works as well if your devices share one token.

## Build and run with Docker

Build the image from the repository root:

```bash
docker build -f ota-hawkbit/Dockerfile -t ota-hawkbit .
```

The image runs as user 65532. Make a state folder that user can write, then run the image with the settings from the table above:

```bash
mkdir -p ota-state
sudo chown 65532:65532 ota-state
docker run --rm \
  -e OTA_ADDON_TOKEN=<plugin bearer> \
  -e OTA_NAMESPACE=<namespace> \
  -e API_URL=https://api.example.com \
  -e HAWKBIT_URL=https://hawkbit.example.com \
  -e HAWKBIT_USER=<user> \
  -e HAWKBIT_PASSWORD=<password> \
  -e OTA_STATE_FILE=/state/ota-state.json \
  -v "$PWD/ota-state:/state" \
  ota-hawkbit
```

`OTA_STATE_FILE` is required. Without it the process stops with `OTA_STATE_FILE is required`. Keeping it on the mounted folder lets a restart reuse the deploy keys.

Apache-2.0.
