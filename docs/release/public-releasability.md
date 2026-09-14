# Public releasability

This repository ships publicly: a module zip, a release tarball, two container
images, and a web UI built from this tree and embedded in the binary. Everything
tracked here is published. This page is the rule that keeps that true, and the
gate that enforces it.

## The rule

No tracked file names the private development fleet. Concretely, three classes:

| Class | What it covers | Use instead |
|---|---|---|
| `lab-address` | An address on the fleet subnet | RFC 5737 documentation addresses — `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24` |
| `lab-host` | A fleet hostname (`vstation-NN`, `dellemc-NN`, the GPU box) | The role, not the machine: `quicbaas.example.com:8443`, `agent-01`, `gpu-host.internal`, `npu-host.internal` |
| `owner-path` | A path under the owner's home directory or the lab's NFS mount | A repository-relative path, or `/var/lib`, `/srv`, `/home/agent` |

The fleet's own subnet, hostnames and path prefixes are written down in exactly
one place: the `patterns` list in `pkg/labstrings/labstrings.go`. Read it there
rather than repeating it here.

Local configuration — the burn-in node's node config, its project bindings, its
deployment tooling — belongs in the private `automatethethings` infra
repository, not here.

Two things are explicitly *not* violations:

- **Benchmark provenance.** "Measured on <host>, 2026-09-12" is the
  measurement's meaning, not a leak. Waive the line.
- **RFC 1918 literals that are not the fleet subnet.** Tests across this tree
  use `192.168.1.x` as an opaque address that is never dialed. The gate does not
  look at `192.168.0.0/16` at large, only at the fleet's own /24.

## The gate

`pkg/labstrings` scans every file in the git index and reports each line
carrying a value in one of those classes. It runs as
`TestTheTrackedTreeIsPubliclyReleasable`, so `make test` runs it — there is no
target to remember and no target list it can fall off. Resolution is textual, so
it works on a tree that does not compile and covers Markdown, YAML, Dockerfiles,
shell, Python and minified JavaScript as readily as Go.

```
go test ./pkg/labstrings/          # the gate, ~1s over 30 MB of tracked content
```

A failure names the file, the line, the class, the matched text, and what to use
instead.

### Accepting a line

Preferred — an **inline waiver**, in the file's own comment syntax, on the
offending line or the line immediately above it:

```markdown
Measured on <host> at sequence length 128: <!-- lab-string-ok: measurement provenance -->
```

```go
Expr: regexp.MustCompile(`...`), // lab-string-ok: the pattern this gate matches on
```

The reason is what makes it a waiver; `lab-string-ok:` with nothing after it is
not one. This is the same mechanism `scripts/localkeygen` and
`scripts/codedsentinel` use, and for the same reason: the justification belongs
next to the value it explains.

Second — an entry in `DefaultAllowList` (`pkg/labstrings/allowlist.go`), for a
whole tree or for a file that cannot carry a comment (minified bundle, captured
log, JSON). Each entry names the classes it accepts and why; an entry with no
reason fails the gate rather than widening it. The list is meant to shrink: what
is in it today is either a test tree using fleet strings as opaque literals, or a
file another change owns.

### Adding a fleet value to the rule set

One entry in `patterns` (`pkg/labstrings/labstrings.go`): the regexp, a
lowercase `Needle` literal every match must contain (the sieve searches for
needles first — the regexps alone run at a few MB/s, the needles at memory
bandwidth), and the guidance a tripping engineer reads. Add a sample to
`TestPatterns_EveryNeedleIsImpliedByItsExpr`, which is what stops a needle from
silently blinding its own pattern.

## What go-quicbaas#944 removed

Seven artefacts committed by accident, now deleted and in `.gitignore`: a
symlink at the repo root pointing into the lab's NFS mount, a stale
`SESSION_RESUME.md`, a per-machine Claude permission allowlist, two FUSE
orphans, and opencode's SQLite WAL/SHM pair.

Lab values in shipping files: seven forge issue links in `docs/barrier/cli.md`
(now `github.com/jeremyhahn/...`), the private registry in
`.github/workflows/release.yml` (now `${{ vars.REGISTRY || 'ghcr.io' }}`, set as
a repository variable on the private forge only), the owner's Gitea principal as
the commit-author placeholder in the shipped UI, the fleet host and address in
`--help` text and exported godoc across `cmd/quicbaas` and `pkg/agent`,
copy-pasteable endpoints in `docs/embeddings/npu.md`, and the private registry in
the captured runner logs under `pkg/cifault/testdata` and the `pkg/sdlc` log
fixtures. The classifier matches on generic signatures — `failed to pull image`,
`http response to https client` — so neutralising the host changed no assertion.
