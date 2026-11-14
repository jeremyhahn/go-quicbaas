### Added

- A live GitHub forge suite (`make integration-test-forge-github`, #2220)
  checks the GitHub adapter against a scratch repository: pull requests, the
  stale-head merge refusal, reviews, commit statuses, labels, webhook
  signatures and branch protection. It needs the `GITHUB_ITEST_*` variables
  and fails, naming each one, when they are unset.
