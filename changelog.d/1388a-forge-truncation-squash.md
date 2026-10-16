### Added

- **A truncated changed-file listing is an error, not a short list.** GitHub's
  3,000-file cap and GitLab's partial diff (`changes_count` `"N+"`) return
  `ChangedFilesTruncatedError` (`ErrChangedFilesTruncated`) from
  `ListPullRequestFiles`. See `docs/sdlc/review.md#listpullrequestfiles`.

### Changed

- **GitLab refuses a merge or rebase on a project whose `squash_option` is
  `always`** with `ForgeUnsupportedError`, before the merge request is
  written (`docs/sdlc/architecture.md#the-forge-seam`).
- **A GitHub binding with no URL defaults to `https://api.github.com`.**
- **`DefaultWebhookEvents` includes `pull_request_review`.**
- **GitHub and Gitea redact the webhook secret from a refused create or update**
  the same way GitLab does.
