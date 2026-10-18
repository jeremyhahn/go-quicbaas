### Fixed

- **The data-plane principal's tenant is deleted when the test process ends
  (#164).** `AuthDataPlane` provisions one tenant per cluster for the whole
  test process, so no `t.Cleanup` could delete it and every run left it
  behind. `helpers.ReleaseDataPlaneTenants` now deletes each such tenant as
  the system admin, and the `batch`, `api/rest` and `api/grpc` integration
  packages call it from `TestMain` after `m.Run`, keeping the exit code and
  reporting a failed delete on stderr.
