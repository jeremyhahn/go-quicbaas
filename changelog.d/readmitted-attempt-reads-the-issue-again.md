### Fixed

- **A readmitted attempt now reads the issue again.** The worker's prompt was
  `Task.Description`, rendered once when the issue was adopted, so an attempt
  readmitted after a park was handed the discussion as it stood on adoption
  day — and never saw the comment that unparked it. That comment is the whole
  mechanism: needs-info is answered on the issue, an operator repair is posted
  on the issue, a ruling is given on the issue, and then the task is
  readmitted. Measured on go-truststrap#24 (2026-09-21): the template the
  worker had asked for was posted verbatim, the task readmitted, and attempt 3
  spent three review rounds insisting the file did not exist. Every attempt
  after the first, and every resumed run, now renders the contract from the
  forge at each turn; a forge that cannot be read leaves the adopted contract
  in place and is audited as `sdlc:contract_stale`.
