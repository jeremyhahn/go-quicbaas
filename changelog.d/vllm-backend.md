### Added

- **A `vllm` backend, registered alongside `ollama` rather than instead of it.**
  Both stay available, so moving a project between runtimes is an edit to its
  binding — and moving back is the same edit.

  vLLM is preferred for throughput because ollama does not batch: measured on
  one fleet at a flat 21 triage turns/min at concurrency 1, 4, 8 and 16 alike,
  with time-to-first-token degrading linearly to 45 s, while vLLM reached 268.7
  turns/min at concurrency 16 with a 1.3 s TTFT. Most of that gap is the
  cross-request prefix cache: with project-stable content first, vLLM gained
  4.5× at concurrency 16 against ollama's 1.06×.

  The three OpenAI-compatible agent endpoints — OVMS, ollama and vLLM — are now
  registered by one loop over their data rather than three near-identical
  blocks. They differ only in route and address.

  Unquantised weights are deliberately NOT recommended: a 4-bit 27B scored
  16/16 on the triage cases at both 6k and 20k tokens, which BF16 cannot beat,
  and BF16 costs an entire 96 GB card.
