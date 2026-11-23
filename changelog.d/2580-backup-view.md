### Added

- Web UI: a Backup view under Cluster lists every backup by ID and type, takes full or incremental backups, verifies and deletes them, and marks backups that captured no shards (go-quicbaas#2580).
- TypeScript SDK: `client.backups` (`BackupClient`, `RestBackupClient`) covers list, create, delete, restore, status and verify over `/api/v1/backups`; `create` takes only the backup type; compression and encryption are the server's configured defaults (go-quicbaas#2580).
