### Changed

- **A spent tool-call budget now asks the model to conclude instead of failing
  the turn.** The agentic loop withdraws its tools for one final round and says
  the budget is gone, so the turn ends in the judgement a human triager makes
  when their time box expires. Measured cause: a triage turn read the CI
  workflow, `go.mod`, the upstream module versions and the implementation —
  every call on point — and then returned nothing because the budget ended
  mid-investigation. A model that is asked outright for a conclusion and still
  produces nothing is reported exactly as before; no partial text is promoted
  to an answer.

### Added

- **`deployments/inference/vllm-cuda/`**, the compose file for serving the
  triage model on a CUDA card. It carries the two flags whose absence is
  invisible until a real job runs: without `--enable-auto-tool-choice` and
  `--tool-call-parser`, vLLM serves plain completions perfectly well and
  answers every agentic turn with a 400. The parser must also match the
  model's chat template — Qwen3.8 emits XML-form tool calls, so `qwen3_xml`
  rather than `hermes`, and a parser that disagrees yields zero tool calls
  rather than an error.
