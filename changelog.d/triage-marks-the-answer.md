### Fixed

- **A re-triage now says why it is one.** An issue triage has already judged
  is judged again only because somebody spoke after that verdict — and what
  they said is the answer to it: the fact needs-info asked for, or the ruling
  an escalation waited on. The discussion was rendered flat, so a small model
  read the old verdict's "blocked on an owner ruling" over the ruling posted
  directly beneath it; measured on go-luks2#12 on the NPU (2026-09-21), which
  re-asked for a ruling the thread already carried. The rendered discussion now
  opens by naming triage's last verdict, marks every comment after it as the
  answer, and the charter says a ruling given there is settled. (Landed in
  `8f1ce591`, whose message wrongly describes it as a gofmt change.)
