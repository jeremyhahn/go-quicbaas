### Fixed

- **An expired request context on a REST range query is a `408`, never an
  empty `200` (go-quicbaas#1326).** The range query handler (`ScanIndex`)
  picked at random between the call's verdict and the expired context, so it
  could answer `200` with no results, turn a service error into a `408`, or
  drop pages it had already received. It now answers `408` only when the
  call was still running at expiry or returned nothing after it. A service
  error still wins, and pages received before a clean completion are
  returned with `200`.
