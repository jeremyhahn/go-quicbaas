### Fixed

- Backups to MinIO, S3 and Azure honor the snake_case storage keys QuicBaaS documents (`access_key`/`access_key_id`, `secret_key`/`secret_access_key`, `use_ssl`, `use_path_style`, `account_name`, `account_key`, `container`, `sas_token`, `connection_string`). go-objstore reads camelCase keys, so before this a MinIO target failed with "accessKey not set" and S3 and Azure targets dropped their static credentials, path style and account settings. An explicit go-objstore key still wins (#1668).
