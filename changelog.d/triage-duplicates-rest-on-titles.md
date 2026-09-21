### Changed

- **Triage is told it has not read the other issues.** The duplicate
  candidates reach the prompt as number and title only, and a small model
  filled the gap by inventing what the other issue "already addresses" —
  go-luks2#141 (a coverage gate) was declared a duplicate of #195 (a
  filesystem probe) twice running, on the strength of both mentioning CI.
  The charter and the candidate list now say the bodies are unread, that a
  shared area is not the same work, that a prerequisite is not a duplicate
  of what it precedes, and that when the titles alone do not settle it the
  issue is judged on its own.
