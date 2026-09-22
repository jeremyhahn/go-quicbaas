### Fixed

- The software-factory worker image now runs a project's own `make build` and
  `make test`: it carries `rsync` (the frontend embed), GNU `grep` (the
  Makefile's checks use `--include`, `--exclude-dir` and `-E`, which busybox
  grep rejects; under busybox the `InsecureSkipVerify` allowlist check passed
  without running), and no longer sets `NODE_ENV=production`, under which
  `npm ci` omitted devDependencies and the TypeScript SDK build failed on a
  missing `tsc` (go-quicbaas #966). `make test-worker-image` probes all three,
  and the allowlist check itself uses `-E` rather than PCRE.
