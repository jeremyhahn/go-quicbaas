### Added

- The WebSocket server is mounted at `GET /api/v1/ws` on the API listener; the upgrade needs the `messaging:subscribe` permission and negotiates the `mqtt`, `amqp` or `stomp` subprotocol (#1691).
