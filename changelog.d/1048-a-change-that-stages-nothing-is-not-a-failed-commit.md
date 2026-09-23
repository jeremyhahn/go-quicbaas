### Fixed

- **SDLC: finished work is no longer refused on "nothing to commit"** (#1048). When
  `git status` reported a change that `git add --all` then staged as nothing — a
  file rewritten to its committed content in between — the runner's commit exited
  1 and refused an attempt whose worker had already committed the real work, so
  the work was never pushed and every retry failed the same way. The publisher now
  asks the index (`git diff --cached --quiet`) after staging, skips the empty
  commit, and publishes the commits the branch already carries; a branch with
  nothing of its own is the ordinary "no changes" outcome.
- **SDLC: a worker's own commits are signed, and an unsigned one is never published** (#1048).
  The checkout now configures the worker's git to sign with the attempt's key, the
  daemon writes an `allowed_signers` file admitting only that key beside the
  workspace spec, and the publisher refuses to push when any commit the branch
  carries beyond its base is not signed by it (`UnsignedCommitError`). An index
  probe that git cannot answer stops the publication before any commit or push.
  The check runs inside every push, the teardown publish of a failed attempt
  included, and it accepts only a good signature whose signer fingerprint is the
  attempt's own SSH key, so a trusted OpenPGP signature does not pass.
  It verifies, and reports the head of, the exact ref being pushed
  (`refs/heads/<branch>`), never `HEAD`.
