### Fixed

- **Closing an embedded-transport stream while its handler is sending no
  longer panics (go-quicbaas#144).** `Close` cancels the stream context
  instead of closing the receive channel, and every handler sends through one
  helper that gives up once the stream is closed, so a send can no longer hit
  a closed channel. `Recv` still drains buffered messages, including a
  handler's final error, before it returns `ErrRecvOnClosedStream`; that error
  is now delivered even when the buffer is full rather than dropped.
