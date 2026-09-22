### Fixed

- The embedded UI's entry page is served `Cache-Control: no-cache` and its
  content-hashed assets `public, max-age=31536000, immutable`. The bundle
  carries no modification times, so browsers received no cache directive at
  all and heuristically kept `index.html`, which names the asset hashes; after
  a deploy the operator's browser ran the previous bundle for hours and read
  the new features as missing (2026-09-22).
