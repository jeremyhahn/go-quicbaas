### Fixed

- Codex turns no longer count cached prompt tokens twice. codex's
  `input_tokens` already includes `cached_input_tokens`, and the ledger
  carried both whole (#1058).
- opencode turns report their token usage. The ACP `session/prompt` result
  carries it, and it was discarded whenever the answer streamed as
  notifications, which is every opencode turn (#1058).
- The direct Ollama backend (`prompt_eval_count` / `eval_count`) and the
  API inference backends (the provider's `usage`) report the tokens their
  servers publish. A response with no counts is still "reported nothing", not
  a measured zero (#1058).
- Consumption ledger entries keep cache-read and cache-write tokens in their
  own fields instead of folding them into input, so a cached turn can be
  priced. Totals, and therefore every quota window, are unchanged (#1058).
- The Cost page's drill-down names projects instead of listing their ids, and
  the breadcrumb names the project it opened. The spend report carries
  `children[].name` and `project_name` on every transport, and
  `cache_read_tokens` / `cache_write_tokens` beside input and output (#1058).
- Negative counts from a producer are repaired one dimension at a time, before
  any are summed or subtracted, and the record is marked repaired. Output 50
  with reasoning -50 no longer becomes a clean zero, and input 100 with cached
  -20 no longer becomes a 120-token prompt.
- The Cost page breadcrumb takes its project name from every accepted report
  and clears it on a tenant switch, so another tenant's project name is never
  left on screen.
