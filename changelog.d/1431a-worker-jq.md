### Changed

- **The software-factory worker image carries jq (go-quicbaas#1431).** Release
  scripts run in a worker need it, starting with go-enclave's `release.sh`.
  The build asserts `jq --version` as the spawn's uid after the sudo proof, and
  both install proofs (the Dockerfile's sudo proof and the runtime
  `fullControlProbe`) now install Debian's `hello`, a package the image does
  not carry, so they still prove an install happened.
