### Changed

- **CI is tiered (go-quicbaas#1266, handbook sdlc §5a).** Every pull request
  runs the PR tier: lint, unit tests, build and security, plus the
  merge-result and snapshot jobs. The full tier (race, web, docker, the FIPS
  build step and `integration.yml`) runs on push to `main`, on
  `workflow_dispatch`, and on a pull request labelled `ci:full`. The label
  takes effect on the pull request's next push or branch update. `CI Gate`
  passes a full-tier job the tier skipped and fails one that was selected and
  did not succeed. `nightly.yml` and `integration.yml` no longer have a
  `schedule:` trigger; the coordinator dispatches the nightly full tier.
  `integration.yml` now triggers on pushes to `main` instead of the
  nonexistent `master` and `develop` branches.
- **Lint's ceiling is 75 minutes, up from 45.** Lint takes 31-41 minutes on
  most runners, and every run on the slowest runner hit 45, which turned
  `main` red with no code at fault.
