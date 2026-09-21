### Fixed

- **A failed verification's output no longer sinks the next prompt.** The
  whole stdout of `make test` / `make lint` — megabytes on a large project —
  was handed back to the worker as round findings; the CLI's context guard
  refused the prompt (`blocking_limit`), and a fresh session got
  `prompt_too_long` from the API on its first turn. Every go-quicbaas attempt
  that failed verification died there (2026-09-21). The evidence is now the
  output's last 16 KiB (`VerificationEvidenceBytes`), whole lines, with the
  omission stated; the full output stays on the job's verification record.
