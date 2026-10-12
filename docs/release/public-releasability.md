# Public releasability

> **Status: REFERENCE** · Last verified: 2026-10-12
> What the public release of this repository ships.

This repository ships publicly: a module zip, a release tarball, two container
images, and a web UI built from this tree and embedded in the binary. Everything
tracked here is published except the private development workflow, which
`.gitattributes` excludes (below). This page is the rule that keeps that true,
and the gates that enforce it.

## The rule

No published file names the private development fleet or its owner. Concretely,
four classes:

| Class | What it covers | Use instead |
|---|---|---|
| `lab-address` | An address on the fleet subnet | RFC 5737 documentation addresses — `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24` |
| `lab-host` | A fleet hostname (`vstation-NN`, `dellemc-NN`, the GPU box, the remote fleet node host) | The role, not the machine: `quicbaas.example.com:8443`, `agent-01`, `gpu-host.internal`, `npu-host.internal` |
| `owner-path` | A path under the owner's home directory, the lab's NFS mount, or the burn-in node's deployment directory | A repository-relative path, or `/var/lib`, `/srv`, `/home/agent` |
| `owner-account` | The owner's account name on its own | A role: `operator`, `agent`, or the module path |

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

Test files are published and scanned like everything else; they use neutral
fixtures (`agent-NN`, `gpu-host`, `198.51.100.x`, `/home/agent`). Only the
gate's own tests (`pkg/labstrings/*_test.go`) carry fleet values, because they
are what the rules exist to find.

## Development tooling stays private

`CLAUDE.md` and `AGENTS.md` (at any depth), `.claude/` and `.serena/` are the
agent development workflow. They name the private forge and internal
identities, so they stay in the private repository and are never published.

The exclusion list is `.gitattributes`: each entry is `export-ignore`, which
every archive git builds applies — `git archive`, and the source tarball a
forge serves for a tag. `.dockerignore` carries the same four entries, so no
image build context includes them. `pkg/labstrings` enforces both:

- `TestThePublishedTreeCarriesNoDevelopmentTooling` — the published manifest
  (`PublishedFiles`: the index minus export-ignored paths) holds no agent file.
- `TestGitArchiveHonoursTheExclusionList` — a repository carrying this tree's
  `.gitattributes`, archived as a forge would, contains none of them.
- `TestTheDockerBuildContextExcludesDevelopmentTooling` — `.dockerignore`
  lists all four.

A new agent tool's files go in `IsDevelopmentTooling`
(`pkg/labstrings/published.go`), `.gitattributes` and `.dockerignore` together.

**Not covered: a history push.** The Go module zip is built from the public
repository's tag and does not honour `export-ignore`; neither does pushing this
repository's history to a public remote, where every commit carries the agent
files. The public tree must be produced as an export (`git archive` of the
release commit) for the exclusion to hold there.

## The gate

`pkg/labstrings` scans every published file (`PublishedFiles`) and reports each line
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
is in it today is the gate's own fixtures, the frontend test fixtures, or a file
another change owns.

### Adding a fleet value to the rule set

One entry in `patterns` (`pkg/labstrings/labstrings.go`): the regexp, a
lowercase `Needle` literal every match must contain (the sieve searches for
needles first — the regexps alone run at a few MB/s, the needles at memory
bandwidth), and the guidance a tripping engineer reads. Add a sample to
`TestPatterns_EveryNeedleIsImpliedByItsExpr`, which is what stops a needle from
silently blinding its own pattern.

## Before tagging: assemble the changelog

Pull requests add `changelog.d/<issue>-<slug>.md` fragments and never edit
`CHANGELOG.md`. Before tagging, run `make changelog VERSION=vX.Y.Z`, which folds
every fragment into a new `## [X.Y.Z] - <date>` section and empties
`changelog.d`, and commit the result; the tag goes on that commit.

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
`--help` text and exported godoc across `cmd/quicbaas` and `pkg/fleet`,
copy-pasteable endpoints in `docs/embeddings/npu.md`, and the private registry in
the captured runner logs under `pkg/cifault/testdata` and the `pkg/sdlc` log
fixtures. The classifier matches on generic signatures — `failed to pull image`,
`http response to https client` — so neutralising the host changed no assertion.
