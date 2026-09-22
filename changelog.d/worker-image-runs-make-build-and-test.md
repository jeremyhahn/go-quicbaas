### Fixed

- The software-factory worker image now runs a project's own `make build` and
  `make test`: it carries `rsync` (the frontend embed), GNU `grep` (the
  Makefile's checks use `--include`, `--exclude-dir` and `-E`, which busybox
  grep rejects; under busybox the `InsecureSkipVerify` allowlist check passed
  without running), and no longer sets `NODE_ENV=production`, under which
  `npm ci` omitted devDependencies and the TypeScript SDK build failed on a
  missing `tsc` (go-quicbaas #966). `make test-worker-image` probes all three,
  and the allowlist check itself uses `-E` rather than PCRE. It also carries
  `pkgconf`, `gtk+3.0-dev` and `webkit2gtk-4.1-dev`, the packages CI's Build
  job installs so `make build` can link the client's embedded desktop GUI;
  without them `build-cli` failed its pkg-config guard in every worker round.
