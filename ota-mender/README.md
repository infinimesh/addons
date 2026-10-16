# Mender

Released in addons v3.4.2. Tested on core v3.4.1 and v3.4.2.

Connects one infinimesh namespace to a Mender open source server. The device is the inventory row whose `OTA_MENDER_ID_ATTR` equals the device uuid. A deployment started in Mender is written to `desired.ota.version` as the artifact name. `inf ota assign` stores `requested_version`, and this addon creates one deployment of that artifact for that device.

```json
{
  "desired": {"ota": {"version": "1.2.0"}},
  "reported": {"ota": {"status": "installing", "error": "", "updated_at": "2026-10-15T00:00:00Z"}}
}
```

You need Docker, core 3.4.1 or newer, the `inf` CLI, and a Mender open source server. This addon is not published as an image. Build it from this repository.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `OTA_ADDON_TOKEN` | | plugin bearer |
| `OTA_NAMESPACE` | | one namespace. Use this or `OTA_NAMESPACES`, not both |
| `OTA_NAMESPACES` | | comma separated namespaces for a public plugin |
| `API_URL` | | core gRPC address. `https://api.example` or, with `OTA_ALLOW_INSECURE=1`, `http://api:8000` |
| `MENDER_URL` | | Mender server, for example `https://mender.example` |
| `MENDER_TOKEN` | | personal access token. Use this or the user and password |
| `MENDER_USER`, `MENDER_PASSWORD` | | login for `POST /api/management/v1/useradm/auth/login` |
| `OTA_ALLOW_INSECURE` | | set to `1` to allow an `http://` `API_URL` or `MENDER_URL`. Without it the process stops with `http:// refused without OTA_ALLOW_INSECURE` |
| `OTA_MENDER_ID_ATTR` | `infinimesh_id` | inventory attribute that stores the infinimesh device uuid |
| `OTA_MENDER_GROUP_TAG` | | one device tag. A device with this tag would need a second static group. Mender allows one, so the device stays in the namespace group and the process logs that once |
| `OTA_POLL_SECS` | `15` | seconds between reads of Mender. `0` reads only when core sends an event |
| `OTA_STATE_FILE` | | file of device ids and deploy keys. A restart reads it and does not post the same deployment again |

Management API only: inventory and `/api/management/v1/deployments`. No hosted-only calls.

## Limits

A device that is not in Mender is `reported.ota.status` `idle` with `error` `not registered in mender`. The process keeps running and looks again on the next event or poll.

Mender keeps a device in one static group. The group is the namespace. A device that also has `OTA_MENDER_GROUP_TAG` stays in that namespace group.

`pending` is `idle`. `downloading` is `downloading`. `installing` and `rebooting` are `installing`. `success` and `already-installed` are `success`. `failure`, `aborted`, and `noartifact` are `failed`.

If Mender does not answer, the process writes `reported.ota.error` `mender unreachable` and keeps running. A rejected `MENDER_TOKEN` (401 or 403, with no user and password) is `mender token rejected`. The process logs `mender rejected the token` once and keeps running. A namespace whose provider is `hawkbit` is refused by core. This process logs `ota provider does not match the namespace` once and does not write that namespace.

A deployment the device has not picked up yet is still the newest one. While it is pending, `reported.ota.status` is `idle` and `desired.ota.version` is that artifact. Assigning a different version aborts that open deployment and creates one for the requested artifact. An older success for the same artifact does not count while a newer deployment is still open.

A missing artifact name is written once as `reported.ota.status` `failed` and is not retried.

## Run

Log in as root, create the plugin, and turn OTA on for the namespace. The plugin bearer is printed once.

```bash
inf login --root
inf plugins create --kind ota --provider mender --title mender --namespace <namespace>
inf namespaces update <namespace> --ota --provider mender
```

`ota cannot be enabled until an OTA addon is registered` means the plugin does not exist yet. Create it first.

`ota provider requires the addon token` means the call used an account token. `desired.ota.version` is written by this process with `OTA_ADDON_TOKEN`.

`ota provider does not match the namespace` means the namespace provider is not `mender`, or this token belongs to another provider. The process logs that once and leaves the namespace alone.

`token is revoked` means the plugin was rotated, its provider changed, or it was deleted. The process logs `token is revoked; run inf plugins rotate and update the secret` and exits non-zero, including when `OTA_POLL_SECS` is `0`. Run `inf plugins rotate <plugin>`, put the new bearer in `OTA_ADDON_TOKEN`, and start again.

```bash
docker compose -f ota-mender/docker-compose.yml up --build
```

Set `OTA_ADDON_TOKEN`, `OTA_NAMESPACE`, `API_URL`, and `MENDER_URL` in that file first. Point `MENDER_URL` at a Mender server you already run. The compose file does not start one.

Assign a version from an account login:

```bash
inf ota assign --namespace <namespace> --device <device> --version <artifact name>
```

The artifact must already exist in Mender.

Apache-2.0.
