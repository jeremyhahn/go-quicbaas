### Added

- The Go SDK re-exports go-qrdb's backup service: `client.Storage().QRDBBackup()` returns `quicbaas.QRDBBackupClient` (go-qrdb's `BackupClient`: create, list, get, delete, restore and verify backups) over the client's QRDB transport, with the `QRDBBackupRequest`, `QRDBBackupInfo`, `QRDBRestoreRequest`, `QRDBRestoreResult` and `QRDBVerifyResult` types. The embedded transport takes the service as `QRDBEmbeddedServicers.Backup` (#1668).
