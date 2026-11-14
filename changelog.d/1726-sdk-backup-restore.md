### Fixed

- Go SDK backup: `Restore` sends the backup service's restore request, so it works on the embedded transport, and returns the restore's job id, status and progress. `Restored` is true only for a finished restore; a running one is not an error. A restore that names a `Source` is refused with an invalid-input error before any call, because the service restores by backup ID only (#1726).
