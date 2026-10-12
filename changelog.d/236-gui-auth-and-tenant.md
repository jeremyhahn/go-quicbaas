### Fixed

- **The desktop UI reports a denial as a denial (go-quicbaas#236).**
  Previously every failed desktop binding call was reported as
  `UNAVAILABLE`, so a 401 or 403 read as an outage, and the rejection reached
  the page as a bare string with no status. A 401 or 403 from the server, or a
  typed unauthorized/forbidden error, now surfaces as `UNAUTHORIZED` or
  `FORBIDDEN`; only a genuine transport failure stays `UNAVAILABLE`. The
  TypeScript SDK's Wails client rejects with the same `APIError` (carrying
  `status`) that the REST client throws, so the dashboard shows "not
  permitted" on both transports.
- **The IAM view and fleet deploy dialog start from your own tenant
  (go-quicbaas#312).** Previously both defaulted to `system`, and the IAM
  tenant selector always offered `system`, so a tenant-scoped operator
  managed, or deployed a node into, a tenant other than their own. Both now
  start from the signed-in tenant. A tenant listing only adds entries to the
  IAM selector, as it already did for the header tenant chip.
