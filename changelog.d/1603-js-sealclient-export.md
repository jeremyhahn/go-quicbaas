### Fixed

- The JavaScript SDK module loads again: `sdk/javascript/src/quicbaas.js` no longer re-exports `SealClient`, a class #1584 removed. Before this fix, the module failed to parse with `Export 'SealClient' is not defined` (#1603).
