### Fixed

- **An expired request context on a REST list or query is a `408`, never an
  empty `200` (go-quicbaas#1326).** When the service call returned no page
  and no error after the request context had expired, the handler picked at
  random between the empty result and the timeout, and could answer `200`
  with an empty page. It now answers `408`. A page or a service error that is
  already available still wins over the timeout, in that order. This covers
  the entity list and query handlers and the KV list handler.
