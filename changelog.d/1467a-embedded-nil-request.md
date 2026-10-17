### Fixed

- **Embedded handlers reject a nil request with a typed error** (#1467). A
  handler given a nil request built its `InvalidRequestTypeError` from
  `reflect.TypeOf(req).String()`, which panics on nil. It now names the type
  through the nil-safe `typeNameOf`, so the caller gets the typed error.
