### Fixed

- Go SDK: over REST and QUIC, `DisableFleetNode` and `EnableFleetNode` call `POST /api/v1/fleet/{id}/disable` and `/enable` instead of retired paths the server answers 404, and `GetSessionHistory` returns Unimplemented instead of posting to a route the server does not have (go-quicbaas#2605).
