### Changed

- **A rate-limited REST request now gets a typed error code (go-quicbaas#158).**
  A 429 from the global REST limiter or the OAuth2 client-authentication
  limiter now has `"code": "rate_limited"` and `"retryable": true` in its body
  and a `Retry-After: 1` header. Before this change the body had no code and
  said `"retryable": false`. Clients can now recognise a limiter refusal
  without parsing the message. Core and module routes return the same body.
