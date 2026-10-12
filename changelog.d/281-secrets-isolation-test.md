### Fixed

- **Deleting a secret you do not have now answers `404`, not `500`
  (go-quicbaas#281).** A delete of a path that is absent from the caller's
  tenant — including one that exists only in another tenant's — reported an
  internal error. It now reports `not_found`, as read, rotate and status
  updates already did.
- **The secrets tenant-isolation tests now test isolation (go-quicbaas#281).**
  Two tenants, each with its own bearer, write the same path; each reads back
  only its own value, and neither can read, list, update or delete the
  other's. The old test wrote two different paths and passed with tenant
  scoping removed.
- **`docs/secrets/cli.md` no longer shows `--tenant-id` as a way to isolate
  secrets (go-quicbaas#281).** The CLI has no such flag. The tenant comes from
  the caller's credential; the API's `tenant_id` field may name only the
  caller's own tenant and selects its DEK.
