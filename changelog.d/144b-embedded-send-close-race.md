### Fixed

- **Calling `CloseSend` on an embedded-transport stream while another
  goroutine is in `Send` no longer panics (go-quicbaas#144).** `CloseSend`
  wakes any blocked `Send` and closes the send channel only once no `Send` is
  in flight. A `Send` that races `CloseSend` either delivers its message or
  returns `ErrSendOnClosedStream`, and the handler still sees end of input
  exactly once.
