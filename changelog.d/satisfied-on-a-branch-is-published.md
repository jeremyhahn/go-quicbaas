### Fixed

- **"Already satisfied" no longer strands work on an unpublished branch.**
  A worker that reads its checkout and finds the change present is right
  about the checkout — but on a resumed attempt the checkout is the
  attempt's own branch, carrying what an earlier attempt wrote and pushed
  and never got to publish. The task was completed as satisfied with the
  code unmerged on a branch no pull request named (go-quicbaas#51,
  2026-09-21: three attempts of work, closed by the fourth). When the branch
  is ahead of its base and no pull request is open, the satisfied verdict
  now sends the branch through verification and publication like any other
  turn's work; `sdlc:satisfied_on_branch` records it.
