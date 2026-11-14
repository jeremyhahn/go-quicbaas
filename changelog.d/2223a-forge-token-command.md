### Added

- `sdlc.forge.token_commands` lets the operator name, per forge type, a command quicbaasd runs for its forge token; the command prints `{"token", "expires_at"}` and is run again before the token expires, so a short-lived token replaces a hand-rotated secret. A binding whose forge type has a command takes its token from it and its `token_secret_ref` is not read (#2223).
