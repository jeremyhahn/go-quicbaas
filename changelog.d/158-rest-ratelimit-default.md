### Changed

- **The REST API now rate-limits every source IP by default (go-quicbaas#158).**
  The global limiter allows 1000 requests per second per source IP, with a
  burst of the same size. Over the limit, a request gets `429 Too Many
  Requests`, which the Go SDK reports as the retryable `CodeRateLimited`. The
  SDK's REST transport retries it within its `MaxRetries` budget only for
  calls with no dedicated route handler, and does not wait for `Retry-After`.
  Set `server.rest.rate_limit` to raise or lower the limit, or set
  it to `-1` to turn the global limiter off. Omitting the key, or setting it
  to `0`, keeps the default. Values below `-1` fail validation. The limit
  applies to the client address that `server.trusted_proxies` resolves.
  Behind a load balancer that is not listed there, all traffic shares the
  balancer's single bucket. The stricter per-source limiter on
  `/oauth2/token`, `/oauth2/revoke` and `/oauth2/introspect` still applies
  with the global limiter on or off.
