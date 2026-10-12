### Added

- **`make changelog VERSION=vX.Y.Z` assembles the release notes before tagging
  (go-quicbaas#474).** It folds every `changelog.d/*.md` fragment into a new
  `## [X.Y.Z] - <date>` section of `CHANGELOG.md`, grouped under the Keep a
  Changelog headings in their standard order (a `### Changed — BREAKING: …`
  heading follows the plain `### Changed`), carries any text left under
  `## [Unreleased]`, and empties `changelog.d` except its `.gitkeep`. A missing
  or non-SemVer version, a version that already has a section, or a malformed
  fragment is refused with the reason, and nothing is changed. The step is
  committed before the tag; `release.yml` does not run it.
