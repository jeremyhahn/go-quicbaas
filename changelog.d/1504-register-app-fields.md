### Fixed

- **App registration through the IAM service keeps `response_types` and
  `resource_prefix`.** `iamServiceWrapper.RegisterApp` now passes the
  request's `ResponseTypes` and `ResourcePrefix` to the app manager, so the
  stored application carries them instead of the defaults (#1504).
