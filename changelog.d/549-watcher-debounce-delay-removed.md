### Removed

- **`watcher.Config.DebounceDelay` and `watcher.ErrInvalidDebounceDelay`
  (go-quicbaas#549).** The field was validated and documented as the
  coalescing window but never read: events for a path always coalesced
  until the next `BatchInterval` flush, whatever `DebounceDelay` held.
  `BatchInterval` is now documented as the watcher's one coalescing window.
  Callers that set `DebounceDelay` drop the line; behaviour is unchanged.
