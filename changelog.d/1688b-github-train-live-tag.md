### Fixed

- The live GitHub merge-train e2e now builds only under the `github_live`
  tag and runs with `make integration-test-train-github`, so the blanket
  integration run no longer fails for want of GitHub credentials (#1688).
