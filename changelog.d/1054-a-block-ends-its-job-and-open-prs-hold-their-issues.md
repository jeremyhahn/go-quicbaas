### Fixed

- Blocking a task now ends the job it has running (go-quicbaas#1054). On the
  node that took the block the job is aborted at once, the way the stall
  detector ends one. On any other node the job's own lease heartbeat re-reads
  the task and aborts it within a third of the lease TTL. Either way the lease
  is released, the container is torn down, and nothing is published. The sdlc
  delivery runner also re-reads its task before every state, every push and
  before opening a pull request, so a block that races a publication wins.
  Both checks fence on the attempt's identity: the job keeps its authority only
  while the task still names it as the open attempt. A block and a readmit that
  both land between two heartbeats therefore still end the old job. Before this
  change the old job would see a pending task and publish anyway. On
  2026-09-22 a task blocked at 23:11 had its job open a duplicate pull request
  at 23:34.
- The `forge_issue` task source no longer adopts an issue that a pull request
  already closes. Two sources count. One is an open pull request whose title or
  body uses a closing keyword (`close[sd]`, `fix(e[sd])`, `resolve[sd]`)
  followed by `#<n>`, `owner/repo#<n>`, or the issue's URL on the bound forge.
  The other is the forge's own issue-to-pull-request link (on Gitea, the
  issue timeline). The skip is recorded once on the audit trail as
  `sdlc:adoption_skipped`, with the pull request's number. Only an open pull
  request holds its issue. The issue is offered again once that pull request
  is no longer open, whether it closed without merging or merged. A merged
  pull request's closing reference therefore never suppresses a reopened
  issue.
- A delivery whose pull request is closed without merging now ends its attempt
  as `superseded` (go-quicbaas#1054). Before, the review gate read the closed
  pull request as a refusal and the runner started another worker round. A
  resumed attempt initialized a worker the same way. Once the rounds ran out,
  the task parked with a question for the owner. Now both paths conclude the
  task `superseded`: it completes with no further round and no question for
  the owner, and one comment on the issue. The task cannot be readmitted. The
  trigger label is removed, and adding it back adopts the issue as a new
  task. On 2026-09-23 task
  3a58725f ran another turn against its closed PR #1052.
