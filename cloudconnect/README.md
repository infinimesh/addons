# Cloud Connect

Writes device state from the Infinimesh API to a CSV spool, then to S3, GCS, or Azure Blob.

Commands: `csvwriter`, `s3`, `cloudstorage`, `blob`. `csvprocessor` is the shared CSV package. Set `USERNAME`, `PASSWORD`, `API_URL`, and the cloud credentials in `docker-compose.yml`. Files are uploaded under their file name and removed from the spool once the upload works. `s3` uses the AWS SDK v2 default credential chain, so `AWS_REGION` and the usual `AWS_*` variables or an instance role work.

Apache-2.0.
