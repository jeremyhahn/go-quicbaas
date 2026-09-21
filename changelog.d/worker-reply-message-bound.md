### Fixed

- **A large worker reply no longer fails the delivery at the wire.** gRPC's
  default 4 MiB receive limit applied between the daemon and its agent
  daemon; one Prompt reply carrying a 35-minute delivery turn on go-quicbaas
  measured 16.9 MB and was refused with `ResourceExhausted` after the whole
  turn had been paid for — then charged to the work and re-dispatched into
  the same wall. Both sides now bound a message at 64 MiB
  (`orchestration.RemoteACPMaxMessageBytes`).
