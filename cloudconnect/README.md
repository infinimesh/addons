# Cloud Connect

Writes device state from the Infinimesh API to a CSV spool, then to S3, GCS, or Azure Blob.

Commands: `csvwriter`, `s3`, `cloudstorage`, `blob`. `csvprocessor` is the shared CSV package. Set `USERNAME`, `PASSWORD`, `API_URL`, and the cloud credentials in `docker-compose.yml`.

Apache-2.0.
