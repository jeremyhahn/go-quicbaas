### Fixed

- **The documented error contract now matches what the server sends
  (go-quicbaas#158).** The REST error body is a flat object (`error`, `code`,
  `details`, `timestamp`, `request_id`, `retryable`), not a nested `error`
  envelope, and its codes are the lower-case `pkg/errors` codes. The error
  table in `docs/multiprotocol-api/api.md` now lists each code with its real
  HTTP status and retryability (`timeout` is 408, not 504), and notes that
  the 400/401/403/404/408/409 responses from the core routes carry no
  `code`. The invented upper-case and nested examples in the scheduler,
  secrets, messaging, Ollama and data-services docs are replaced. A test
  pins the REST error fields and checks the doc table against `pkg/errors`.
  The wire format is unchanged.
