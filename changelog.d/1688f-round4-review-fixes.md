### Fixed

- A forge credential refresh whose delivery to the container failed is
  retried by the next refresh, even when the token source answers the same
  token (#1688).
- A release-blocker or release-candidate issue now ranks above every
  non-release issue, a release issue at priority:low included (#1688).
- The ai-native profile lands docs and ci changes through a pull request too:
  a strategy's direct-push carve-out does not apply under it (#1688).
