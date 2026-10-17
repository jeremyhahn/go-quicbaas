### Security

- **The merge-result test image is built with `--pull --no-cache`.** Its
  `apk upgrade` layer was reused from the runner's cache, so the Trivy gate
  failed on CVE-2026-77214 (libexpat 2.8.5-r0) after Alpine had shipped the
  fix (2.9.0-r0). Every build now upgrades against the current Alpine index,
  as the integration compose build already does (#1501).
