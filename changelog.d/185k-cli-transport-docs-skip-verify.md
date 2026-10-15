### Fixed

- The CLI docs' "Transport selection" section describes what the CLI does
  (go-quicbaas#185), including the `.sock` switch to `unix` under `grpc`
  (CLI) and the switch to REST for an `http://` or `https://` address under
  `grpc` (SDK client).
- The orchestration CLI examples and the CLI agent instructions no longer
  name `--tls-skip-verify`, which does not exist. Certificate verification
  is never bypassed; the examples trust the server with `--tls-ca`.
