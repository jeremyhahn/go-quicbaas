### Changed

- **Triage stops asking after two unusable replies at one head.** A reply set
  aside leaves no mark on the issue, so the issue is a candidate again on
  every pass — and a model that fails an issue the same way each time was
  asked every fifteen minutes for as long as the head stood. The runner now
  counts set-asides per issue and default-branch head, skips the issue after
  two (audited as `sdlc:triage_skipped`), and asks again only when the head
  moves or somebody comments, both of which change what the model is shown.
