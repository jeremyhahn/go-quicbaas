### Security

- Release workflow: the Trivy image gates scan a saved image tarball instead of mounting the runner's Docker socket, every release job runs on the release runner expression, and the scanner and build images are pinned by digest (#1171).
