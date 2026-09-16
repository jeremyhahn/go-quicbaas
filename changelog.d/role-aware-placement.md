### Added

- **A runtime-tunable role policy for scarce accelerators.** A pool answers
  "which profile", which is only half the question: it cannot decide who goes
  FIRST when one place is worth more to one kind of work than another. On a
  fleet with one large accelerator and several small ones, that is the question
  that matters — the large card is where review belongs, and triage would
  happily fill it and starve the thing it was bought for.

  `AgentProfile.Roles` states who a profile is for and on what terms it helps
  everybody else: `prefer` names the role it is held for, `reserve` keeps slots
  empty for that role's *latency*, and `exclusive` refuses everyone else
  outright. Selection is `ProfileResolver.ResolveForRole`, and the two tests are
  separate: capacity is arithmetic about what is running, the policy is a
  statement about what the profile is FOR, and a profile with a free slot can
  still decline so the work spills to the next profile in the pool.

  **Idle capacity is shared, not reserved.** A profile preferred by review does
  not sit empty while triage queues — it yields whatever review is not asking
  for, measured as queued jobs rather than running ones. Reserving
  unconditionally would trade one starvation for another. The policy rides on
  the stored profile record, so retuning the split is an edit rather than a
  redeploy, which matters because the right split depends on a queue that
  changes hourly.

  An unreadable queue admits the work: refusing on a failed read would stop the
  fleet on a storage blip, and the concurrency cap still bounds what starts. A
  profile with no policy behaves exactly as before.
