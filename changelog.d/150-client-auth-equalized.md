### Security

- **Client ids cannot be told apart by latency at client authentication
  (#150, #151, #152).** An unknown client id, a client record that does not
  decode and a known client now each pay the same secret derivation, because
  the secret is checked before the application is read.

### Fixed

- **A client store outage at the token endpoint answers `server_error`
  (#153).** When the client store cannot be read, the token endpoint now
  answers `server_error` instead of telling the client its credentials are
  invalid. Every client-attributable failure still answers `invalid_client`.
