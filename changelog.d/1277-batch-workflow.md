### Added

- **`batch.yml`: fuzz, race-full, soak and bench on the batch runners
  (go-quicbaas#1277).** Adopts the handbook's batch workflow template v1. It is
  dispatch-only and in no merge gate; it runs on the `batch` runner label, so
  it never takes a CI slot from a pull request. `race-full` runs `./pkg/...
  ./cmd/...` three times under the race detector with the unit suite's full
  tag set, `soak` loops `make integration-test` (Ollama and GPU suites
  excluded), and `bench` runs `make benchmark`'s scope with `-count=5` for
  benchstat. Findings are printed as `BATCH-FINDING` log lines. The repo has
  no `Fuzz*` targets yet, so the fuzz job finds none and passes.
