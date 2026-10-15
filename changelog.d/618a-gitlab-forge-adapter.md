### Added

- **SDLC projects can bind to GitLab (go-quicbaas#618).** A binding with
  `forge.type: gitlab` is now accepted: the GitLab adapter implements the
  whole forge surface (issues, labels, merge requests, approvals, pipeline
  jobs and commit statuses, job rerun, cancel and logs, webhooks, onboarding
  and collaborators) against the GitLab REST API v4, on GitLab.com or a
  self-managed instance's API root. A merge names the head it was reviewed at
  and is refused, never retried, when the head moved. Deliveries are verified
  by comparing `X-Gitlab-Token` with the webhook secret in constant time.
  GitLab cannot record a request-changes verdict, so the module does not
  submit reviews there, and a protected branch names no required status
  context, so branch protection is reported as unsupported.

### Security

- **A webhook secret GitLab echoes back is redacted.** When GitLab refuses a
  webhook write and quotes the request in its answer, the secret that
  request carried is scrubbed before the refusal reaches an error or a log.

### Fixed

- **GitLab approvals count again.** GitLab's approvals answer names who
  approved but not when, so no approval could be bound to a head and every
  merge request waited forever for approvals. An approval is now bound
  through the system note GitLab writes when it is given, to the head that
  was current just before; one that cannot be bound still does not count.
- **Merge-train comments on GitLab reach the merge request.** GitLab numbers
  merge requests apart from issues, so the train's landing, refusal and
  ejection comments, posted through the issue discussion, landed on an
  unrelated issue sharing the number or failed. The forge seam now has
  `CommentOnPullRequest` and `ListPullRequestComments`; GitLab serves them
  from the merge request's notes, and Gitea and GitHub, which share one
  numbering, from the issue discussion as before.
- **GitHub Actions cancel, rerun and logs reach the right job.** The adapter
  sent a check run's id to the Actions job routes, but GitHub does not give
  a job the id of its check run, so those calls hit another job or none. The
  job is now resolved through GitHub's own data — the check run's suite, the
  workflow run that owns it, and the job whose `check_run_url` names the check
  run — and the job routes get the job's own id.
- **Pull-request labels on GitLab reach the merge request.** The park and
  unpark labels on a reviewed pull request and the train's ejection label
  went through the issue label routes, so on GitLab they landed on an
  unrelated issue sharing the number or failed. The forge seam now has
  `AddPullRequestLabels` and `RemovePullRequestLabel`; GitLab edits the merge
  request's labels and reads them back, and Gitea and GitHub use the issue
  labels as before. A pull request now reads back with its labels, so
  unparking removes them in the forge's own spelling.
