### Fixed

- WebSocket upgrades at `GET /api/v1/ws` are no longer blocked by the request timeout (#1691).
- The MQTT subprotocol handles every packet in a WebSocket message and packets split across messages (#1691).
- An upgrade that races server shutdown is closed instead of registered (#1691).
- AMQP publications are routed through exchange bindings: direct, fanout and topic exchanges, with the default exchange routing to the queue named by the routing key (#1691).
