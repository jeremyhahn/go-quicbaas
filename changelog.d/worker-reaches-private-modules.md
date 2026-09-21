### Fixed

- **A worker can build a project that depends on private Go modules.** The
  worker image's Go asked the public proxy for `github.com/<owner>/<repo>`
  and was told it did not exist, so verification failed before the change
  compiled; measured on go-quicbaas#53 as three review rounds of one
  dependency-resolution failure and an escalation. The binding's new
  `modules.rewrite_prefix` puts `GOPRIVATE` and a git `insteadOf` rewrite to
  the project's own forge remote in the worker's environment — over ssh, with
  the key and host key the container already holds, so nothing secret travels
  in it. A project naming no prefix is untouched.
