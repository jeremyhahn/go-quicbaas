### Fixed

- The SDK backup client's `Restore` no longer drops `RestoreRequest.Source`. The backup service restores only by backup ID, so `Restore` now requires `BackupID` and refuses any request that sets `Source` with an invalid-input error before it calls the server; before, a source-only restore went out with an empty backup ID and the server refused it. `RestoreResponse` gains the `JobID` and `Status` fields for the restore job the service reports; `Restored` means a finished restore, and a running restore is not an error (#1726).
