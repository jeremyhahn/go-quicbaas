### Added

- Web UI: a Backup view under Cluster lists every backup by ID and type, verifies and deletes them, marks backups that captured no shards, and keeps backup creation turned off until the daemon's backup service captures the cluster's data (go-quicbaas#2580).
- TypeScript SDK: `client.backups` (`BackupClient`, `RestBackupClient`) covers list, delete, restore, status and verify over `/api/v1/backups`; it has no create until the daemon's backup service captures the cluster's data (go-quicbaas#2580).
