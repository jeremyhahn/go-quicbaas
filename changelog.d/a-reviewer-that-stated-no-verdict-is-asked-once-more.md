### Fixed

- **A reviewer reply that stated no verdict no longer throws the whole review
  away.** Measured on the burn-in fleet (2026-09-22, PR #983, review job
  `c64bd251`): the reviewer had read the entire diff and ended its turn on a
  progress note — "I'm finishing the checks around forge mergeability" — with
  no `SDLC-REVIEW:` declaration. The review runner refused it, failed the job
  and closed the session, and the review sweep then spawned a fresh container
  to re-read the same change from nothing. When a reply carries no declaration
  at all, the **same** reviewer session is now prompted once more — "Your
  reply stated no verdict. Finish your checks and end with the `SDLC-REVIEW:`
  declaration and findings." — audited as `sdlc:review_reprompted` and
  counting as neither a review round nor a new review job. The reminder
  reaches the same *conversation*: the codex exec transport now resumes the
  thread its first turn started (`codex exec resume <thread> -`), so the
  reviewer still holds the charter, the change and its own analysis. The
  reminder runs under a real deadline, `ReviewTurnTimeout` (one hour), and a
  reviewer that never answers it is cancelled, failing the job with
  `ReviewRepromptTimeoutError`. A second declaration-less reply is
  refused exactly as before, as are the two refusals that are not a missing
  declaration: an unknown code, and a declaration with nothing above it.
