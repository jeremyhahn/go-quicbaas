### Fixed

- **A slow forge no longer fails work that succeeded, or charges the task for
  it** (go-quicbaas#1034). Every Gitea call ran under one 30-second client
  timeout; under CI load a triage listing, a review read and a squash merge
  missed it — and the merge had landed, so the job was recorded failed on a
  change that was on `main`. A create-PR that timed out after Gitea opened the
  pull request would have been refused as a duplicate on retry.
  - Per-call deadlines: reads `sdlc.forge.read_timeout` (30s), light writes
    `write_timeout` (1m), merge and create-PR `heavy_write_timeout` (5m).
  - Unanswered reads (timeout, dropped connection, 502/503/504) are retried up
    to `read_attempts` (3) with backoff from `retry_backoff` (1s); exhaustion is
    a typed `ForgeRetriesExhaustedError`. A status the forge itself returned is
    final.
  - Writes are never blindly repeated. An unanswered merge re-reads the pull
    request (merged is success; open and mergeable goes to the bounded merge
    retry). `CreatePullRequest` adopts the open pull request for the same
    head -> base from the same repository (a fork's same-named branch is
    never adopted; `ForgePullRequest.HeadRepo` carries the source) — before creating, after an unanswered create, and on Gitea's
    409 duplicate refusal. Idempotent, read-back-verified writes (labels,
    status, close) are replayed; the rest surface
    `ForgeWriteUnconfirmedError`. One table, `giteaWritePolicies`, holds every
    write's policy.
  - A job that ends on a forge that did not answer is classified
    `forge_unreachable`: refunded and requeued under the consecutive-fault
    bound instead of charged to the work.

### Changed

- `sdlc.ForgeFactory` and `sdlc.NewForgeBackend` take the node's
  `sdlc.ForgeTransport` as a third argument; a zero value takes the defaults.
