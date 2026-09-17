### Fixed

- **The reviewer was told to verify acceptance criteria and never given them.**
  Its charter says to check the change against the issue it closes; the review
  prompt carried the pull request's title, body and file list but not the
  issue, and the reviewer's container holds no forge credential — so every
  attempt to fetch one came back 404. The effect was not a worse review but
  **no verdict at all**: on go-luks2 #196 the reviewer read all 21 changed
  files, found no defect, and still declined to approve, because it could not
  confirm the change closed the issue. A change with nothing wrong with it
  could not merge.

  The issue and its comments are now read on the control plane, where the
  forge token works, and rendered into the prompt. The comments matter as much
  as the body: an owner's ruling on a question the worker raised lives in a
  comment, and a reviewer judging against the body alone contradicts decisions
  already made. Both are bounded so a long discussion cannot crowd out the
  diff, and the bound keeps the newest comments, because a ruling supersedes
  the question it answers.

  When the issue cannot be read the prompt says so, names why, and tells the
  reviewer to judge the code anyway and mark conformance unverified — a
  missing input must not become a refused review.
