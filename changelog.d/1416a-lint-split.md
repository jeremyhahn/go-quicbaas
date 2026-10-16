### Changed

- **`make lint` is split into `make lint-pre` and `make lint-go` (#1416).**
  `lint-pre` runs the prerequisites (`vet-integration`, `verify-naming`,
  `check-nolint`, `secure-analyzer`, `secure-analyzer-self-test`); `lint-go`
  runs golangci-lint alone with its `--timeout=20m` and signal-kill triage.
  `make lint` is still their composition, with the same output and exit
  codes, so CI is unchanged. Both share one canned recipe. The Makefile
  comments record the wall times measured on a loaded lane node.
- **`make secure-analyzer` is split per tag set (#1416).** It took 616 s on
  a loaded lane node, past one 600 s call, so each unit tag set has its own
  target (`secure-analyzer-maximal`, `-standard`, `-lean`, `-minimal`,
  `-desktop`) and the integration, hardware and Go SDK integration passes
  run as `secure-analyzer-integration`. `secure-analyzer` is their
  composition, so `make lint-pre` and `make lint` run the same checks with
  the same exit codes. The other prerequisites already fit on their own:
  `vet-integration` 153 s, `secure-analyzer-self-test` 89 s, `verify-naming`
  and `check-nolint` about 1 s.
