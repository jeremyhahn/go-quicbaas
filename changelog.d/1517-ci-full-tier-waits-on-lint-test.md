### Changed

- **The full-tier CI legs wait on lint and test (#1517).** `test-full`, `race` and `web` now need `[merge, lint, test]` (`merge` stays for its `base_sha` output) and `docker` needs `[lint, test]`, so a red lint or test skips them instead of spending runner time on a head that cannot merge.
