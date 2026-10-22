### Fixed

- `TestStreamingBatchConcurrentSessions` no longer hangs at `wg.Wait` until the
  package timeout when a session cannot connect: each session goroutine defers
  `wg.Done`, dials through the error-returning `dialStreaming`, and reports the
  failure through its results channel. `helpers.DataPlaneGRPCCredentialsE`
  returns the data-plane enrollment error instead of calling `t.Fatalf` off the
  test goroutine (#1593).
- `TestStreamingBatchSessionDialFailure` guards that fix without a cluster: the
  session worker `runStreamingSession` takes its dialer, and the test hands it
  one that always fails and requires every session to finish with exactly one
  error result before a deadline (#1593).
