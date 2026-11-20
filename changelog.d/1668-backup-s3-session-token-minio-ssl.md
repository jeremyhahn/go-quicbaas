### Fixed

- S3 backups with temporary credentials (`session_token` beside `access_key` and `secret_key`) sign every request with the session token. go-objstore's S3 backend dropped the token, so S3 rejected those backups and restores (#1668).
- `use_ssl` decides the scheme of an S3 or MinIO endpoint given as a bare `host:port`: `false` uses HTTP, `true` HTTPS; unset, MinIO uses HTTP and S3 HTTPS. An endpoint whose scheme contradicts an explicit `use_ssl` is refused. Before this a MinIO target with `use_ssl: false` still connected over HTTPS (#1668).
