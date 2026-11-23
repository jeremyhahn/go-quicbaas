### Fixed

- Go SDK: over REST and QUIC, `DisableFleetNode` and `EnableFleetNode` call `POST /api/v1/fleet/{id}/disable` and `/enable` instead of retired paths the server answers 404 (go-quicbaas#2605).
