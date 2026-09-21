### Changed

- **An OpenVINO turn now says what it did.** Every command the tool loop ran
  is reported as a `tool_use` block ahead of the answer, and an answer forced
  on the final, tool-less round carries the stop reason `tool_budget_spent`
  instead of `end_turn`. The triage audit line records both. Before, a turn
  that ran twelve commands and then gave a 27-byte answer was
  indistinguishable from one that answered from the prompt alone, which is
  why the NPU's give-ups on go-luks2 could not be told from its judgements.
