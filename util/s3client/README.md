# Custom S3 endpoints

The DAS server (`anytrustserver`) supports S3-compatible services with:

```sh
--data-availability.s3-storage.endpoint=https://cos.ap-singapore.myqcloud.com \
--data-availability.s3-storage.use-path-style=false \
--data-availability.s3-storage.region=ap-singapore \
--data-availability.s3-storage.bucket=example-bucket-1250000000
```

Set `use-path-style=false` for Tencent COS buckets requiring virtual-hosted
addressing. Supply the regional service endpoint, without a bucket name or object
path; the SDK adds the bucket to the hostname. Configure credentials and the
object prefix separately.

For custom endpoints, omitting `use-path-style` preserves path-style addressing,
including existing MinIO configurations. With no custom endpoint, the setting is
ignored and the AWS SDK's existing endpoint behavior is preserved. Other users
of the shared S3 client expose the same option under their own configuration
prefixes.

The equivalent native DAS JSON setting is
`data-availability.s3-storage.use-path-style: false`. A custom startup script that
reads a separate `nodeConfig.json` must explicitly forward the setting as a CLI
argument; adding a field to that script's input file alone has no effect.

Changing addressing style does not change object keys or contents and does not
backfill local data. After deploying a rebuilt image, check `/health` for bucket
access, then verify a new batch upload and a read directly from the destination
storage. A successful health check does not establish object write permission.
