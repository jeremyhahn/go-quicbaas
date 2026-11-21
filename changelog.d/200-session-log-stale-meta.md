### Fixed

- A session's stored events can no longer be overwritten after a failed
  metadata update. `KVSessionEventLog.LatestSequence` treats the metadata's
  sequence as a starting hint and reads past it to the last stored event, and
  `Append` writes each event create-if-absent where the store supports it, so
  an append onto an occupied sequence is refused with `ErrSessionLogGap`
  instead of replacing the event — the terminal `KindSessionClosed` included
  (#200).
