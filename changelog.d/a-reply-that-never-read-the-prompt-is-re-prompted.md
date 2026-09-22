### Fixed

- **A worker reply that never read the prompt no longer costs the task a
  round and an attempt.** A persistent worker session can hold a turn's worth
  of its own — a background task armed two turns earlier — and answer THAT
  when a review round arrives. Measured on the burn-in fleet (2026-09-22):
  451ms, one text block, no tool use, nothing changed, and the runner took
  the "claimed work on an unchanged tree" path, so the job failed, the attempt
  was charged, and the task lost its pull-request adoption state and a review
  round to a reply that never addressed the prompt. A turn that declared no
  stop, used no tool, changed no file and came back inside
  `NonAnswerTurnBound` is now re-prompted once with the same prompt plus one
  line saying the previous reply did not address it, audited as
  `sdlc:non_answer_reprompted`, counting as neither a new round nor a new
  attempt. The second consecutive one is charged exactly as before.
