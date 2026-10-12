### Fixed

- **The Go and JavaScript SDKs now call quicbaas's own session verbs,
  `POST /api/v1/iam/{refresh,validate,logout}` (go-quicbaas#259, closes the
  subset filed as #231).** Validate and logout send the access token in the
  `Authorization: Bearer` header, not the body, so validate, logout and
  renewal now work on a composed node. Before this, the SDKs called the
  xkms-owned `/api/v1/auth/*` paths, so validate was rejected, logout
  revoked nothing, and renewal failed. The Go REST transport also no longer
  clears a caller-configured `Authorization` header after `ValidateToken`.
