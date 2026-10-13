### Fixed

- **A disconnecting client can no longer crash the server with "send on
  closed channel" (go-quicbaas#1284).** The gRPC PEP `Subscribe` handler
  closed the updates channel on return while the policy service goroutine
  could still be sending on it. The handler now leaves the channel to its
  sender, and it cancels the context it passes to the service so the service
  goroutine always exits. The WebSocket MQTT, STOMP and AMQP handlers had the
  same defect: they closed their delivery channel while a hub publish, which
  runs subscriber callbacks outside the hub lock, could still be sending on
  it. They no longer close it.
