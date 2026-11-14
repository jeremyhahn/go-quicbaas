### Fixed

- A batch put or delete with no tenant bound is now refused with 403 / PermissionDenied, as single-item operations are, instead of being reported as invalid input (#1800).
