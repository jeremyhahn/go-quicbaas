### Changed

- **The PR tier runs two unit-test legs, not six (go-quicbaas#1272).** A plain
  pull request push runs lint, build, security, Merge Result, Merge-Result
  Snapshot, the `default` and `sdk-go` unit-test legs, and `CI Gate`. The
  `standard`, `lean`, `minimal` and `coverage` legs move to the full tier
  (job `test-full`), so the coverage threshold holds on `main` and on a
  `ci:full` pull request rather than on every push.
- **Adding `ci:full` starts a full-tier run at once.** The `pull_request`
  trigger of `ci.yml` and `integration.yml` now carries the `labeled` type.
  Any other label also starts a run, which re-runs the PR tier on the same
  commit; the ref-keyed concurrency group cancels the run it supersedes.
- **Every `CI Gate` check is tagged `pr` or `full`.** A skip passes only for a
  `full` job when the full tier was not selected; every other skip fails the
  gate. `test/buildgates` (run by `make test`) asserts that every job is in
  the gate's `needs` with a tagged `check` line, that the tags match each
  job's `if:`, that every `pull_request` workflow has a ref-keyed
  cancel-in-progress group, and that no workflow but `automerge.yml` carries
  a `schedule:` trigger.
