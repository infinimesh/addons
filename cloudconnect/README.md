# Cloud Connect

Writes the reported state of every device the account can see from the infinimesh API to CSV files, then uploads the files to S3, Google Cloud Storage or Azure Blob.

It has four commands:

- `csvwriter` streams state from the API and writes CSV files to a spool folder
- `s3`, `cloudstorage` and `blob` upload finished files from the spool and delete them

`csvwriter` writes one folder per device and one file per minute, named `<unix time of the end of the minute>.csv`, for example `/data/<device uid>/1791792480.csv`. Each reported state is one line:

```csv
f3913526-38d9-4bf2-a60d-b616d6ec8dcc,pump-7,device,2026-10-12T08:19:51.617Z,,"{""hum"":40,""temp"":20}"
```

The columns are device id, name, kind, timestamp, version and the data as JSON. There is no header line, and `version` is always empty.

The uploaders look at the spool every 5 seconds. They upload each file that has not changed for one minute, and delete it once the upload works.

## Settings

`csvwriter`:

| Variable | Default | Meaning |
|---|---|---|
| `USERNAME`, `PASSWORD` | | infinimesh account |
| `API_URL` | | infinimesh gRPC address, see the main README |
| `TOKEN_REFRESH_INTERVAL_SECS` | none, required | how often to log in again |
| `OBJECT_REFRESH_INTERVAL_SECS` | none, required | how often to look for new or removed devices |
| `WRITE_DIR` | | spool folder. It must end with `/`. |

All uploaders:

| Variable | Default | Meaning |
|---|---|---|
| `READ_DIR` | | spool folder, the same as `WRITE_DIR` |

`s3`:

| Variable | Default | Meaning |
|---|---|---|
| `AWS_S3BUCKETNAME` | | bucket. It is created on start if possible. |
| `AWS_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | | read by the AWS SDK v2 default chain, which also takes profiles and instance roles |

`cloudstorage`:

| Variable | Default | Meaning |
|---|---|---|
| `GCP_PROJECT_ID` | | project the bucket is created in |
| `GCP_BUCKET_NAME` | | bucket. It is created on start if possible. |
| `GOOGLE_APPLICATION_CREDENTIALS` | | path to a service account key file |

`blob`:

| Variable | Default | Meaning |
|---|---|---|
| `AZ_ACCOUNT_NAME`, `AZ_ACCOUNT_KEY` | | storage account and its shared key |
| `AZ_CONTAINER_NAME` | | container. It is created on start if possible. |

## Limits

- `s3` and `blob` name each upload by its file name only, without the device folder. Files of two devices from the same minute get the same name, and the second upload replaces the first.
- `cloudstorage` names each upload by its full spool path, for example `/data/<device uid>/1791792480.csv`.
- `cloudstorage` does not check the error when it closes an upload, so a failed upload can still delete the local file.
- Not tested live. `csvwriter` uses the same SDK calls as `timeseries`, which was tested against core. The S3, Google Cloud Storage and Azure uploads have not been tried against real accounts.

## Run

```bash
docker compose -f cloudconnect/docker-compose.yml up --build
```

The compose file starts `csvwriter` and all three uploaders on a shared `./data` folder. Delete the uploaders you do not need. Set `USERNAME`, `PASSWORD`, `API_URL` and the cloud values first. For `cloudstorage`, change `CHANGEME.json` to `./your-key.json`. Without the `./`, Compose treats it as a named volume and not as a file.

Apache-2.0.
