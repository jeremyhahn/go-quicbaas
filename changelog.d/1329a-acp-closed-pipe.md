### Fixed

- **A CLI agent that exits mid-prompt reports its exit, not "file already
  closed" (go-quicbaas#1329).** The session supervisor of a persistent
  stream-json CLI agent reaped the process as soon as it exited, and
  `exec.Cmd.Wait` closed the stdout pipe while the stream reader was still
  draining it, so the prompt failed with the untyped `read |0: file already
  closed` and lost the agent's last output. The agent's stdout is now a pipe
  the backend owns and only the stream reader closes, so a prompt sees every
  record the agent wrote: an `is_error` result still reports `ErrStreamJSON`,
  and a stream that ends without a result reports
  `ErrStreamJSONProcessExited` with the exit code and stderr tail.
- **A CLI agent that exits behind unread output is still reaped
  (go-quicbaas#1329).** The supervisor reaps the agent the moment it exits
  instead of waiting for its stdout to be drained, so a prompt that timed out
  with the reader's buffer full no longer leaves the process unreaped, the
  session active, and `ProcessExitObserver` never called. The output the
  agent left stays readable to the end of the stream.
