### Fixed

- **A worker prompt can no longer hang forever behind a slow handshake.** The
  stream-json open started one goroutine to wait for the agent's init record,
  another to drain the handshake reply, and every prompt started one more per
  line — each returned to its caller on a timeout while the goroutine it had
  started was still reading. A handshake whose API calls were being retried
  for four minutes outlived the two-minute drain bound, the first prompt's
  reader then raced it on the same scanner, and the prompt's own `result` went
  to a channel nobody read: two go-quicbaas jobs held their slots for an hour
  (2026-09-21). One reader per session now feeds one channel, the session
  counts the replies still owed, and a prompt settles them before it writes.
