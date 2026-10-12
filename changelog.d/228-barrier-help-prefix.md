### Fixed

- **CLI help examples now name commands that exist (go-quicbaas#228).** The
  per-tenant barrier help showed `quicbaas tenant barrier ...`; the group
  lives under `iam tenant`, so the examples now read
  `quicbaas iam tenant barrier ...`. The `scheduler job` verbs showed
  `quicbaas scheduler <verb>` without `job`, and the top-level help named
  `rag ingest`, `kms create-key` and `secrets store`, none of which exist;
  they now show `rag query`, `kms key generate` and `secrets put`. A test
  resolves every `quicbaas ...` example in the CLI's help against the real
  command tree, so a wrong path fails the build.
