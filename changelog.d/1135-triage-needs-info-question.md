### Fixed

- **A triage needs-info verdict must ask a question, and a duplicate must
  share something with what it duplicates (go-quicbaas#1135).** A needs-info
  reply now asks its questions on `SDLC-TRIAGE-QUESTION:` lines (at most
  five, each cut to 300 characters), and the posted comment lists them
  numbered. A needs-info reply with no question, such as a summary of the
  issue under "What is missing:", is set aside as unusable and asked again,
  with the existing retry bound, instead of parking a well-scoped issue that
  nobody can answer. A duplicate verdict is set aside when the two issues
  name no file path or backticked identifier in common. The hidden verdict
  marker now also records `model=` (the model the triage session reported,
  `unknown` when none) and `prompt=` (`TriagePromptRevision`, a hash prefix
  of the prompt text that a test pins), so mis-triage can be counted per
  model and prompt. Comments posted before the change still parse.
