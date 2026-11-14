### Added

- `pkg/testutil/githubitest` holds the scratch-repository fixtures for live GitHub tests: a fail-closed `GITHUB_ITEST_*` environment gate, a REST client that cuts branches, commits files, opens, approves and cleans up pull requests, and the `queue-ci.yml` workflow whose `ci-gate` job fails on a `BREAK-CI` file. The integration-tagged `cmd/quicbaasd` merge-train e2e uses them to land a train with an adopted pull request on GitHub and to eject a car that breaks the queue CI (#2221).
