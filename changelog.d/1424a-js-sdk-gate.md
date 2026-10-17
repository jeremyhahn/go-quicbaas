### Added

- **`make test-sdk-js` runs the JavaScript SDK's tests and lint
  (go-quicbaas#1424).** It installs `sdk/javascript` from its tracked lockfile
  with `npm ci`, then runs `npm test` and `npm run lint`. Lint now fails on
  warnings (`--max-warnings 0`), so the gate holds the clean baseline.

### Fixed

- **The `test/quicbaas.test.js` suite loads again.** The ClusterDEK client was
  deleted, but `src/quicbaas.js` and `src/index.js` still re-exported
  `NodeDEKSyncStatus`, `ReencryptionState` and `RecoveryMethod`. Exporting an
  undeclared binding is an ES module syntax error, so Babel refused the file
  and Jest reported a parse error. The stale exports, the README entry and
  the test of the removed `clusterDEK()` accessor are gone.
- **`eslint src test` reports no problems.** `eslint --fix` cleared the
  formatting rules; the rest were unused imports and parameters, a useless
  regex escape, a duplicate `onmessage` key in a test double, and calls to
  `fail()`, which Jest's default runner does not define.
