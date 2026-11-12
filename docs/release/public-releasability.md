# Public releasability

> **Status: REFERENCE** · Last verified: 2026-10-15
> What the public release of this repository ships.

This repository ships publicly: a module zip, a release tarball, two container
images, and a web UI built from this tree and embedded in the binary. Everything
tracked here is published except the private development workflow, which
`.gitattributes` excludes (below). This page is the rule that keeps that true,
and the gates that enforce it.

## The rule

No published file names the private development fleet or its owner. Concretely,
six classes:

| Class | What it covers | Use instead |
|---|---|---|
| `lab-address` | An address on the fleet subnet | RFC 5737 documentation addresses — `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24` |
| `lab-host` | A fleet hostname: the build and runner servers, the GPU and NPU workstations, the remote fleet node host | The role, not the machine: `quicbaas.example.com:8443`, `agent-01`, `gpu-host.internal`, `npu-host.internal` |
| `owner-path` | A path under the owner's home directory, the lab's shared network mount, or the burn-in node's deployment directory | A repository-relative path, or `/var/lib`, `/srv`, `/home/agent` |
| `owner-account` | The owner's account name on its own | A role: `operator`, `agent`, or the module path |
| `coord-host` | The fleet's coordination services: forge and registry endpoints, the agent accounts that use them, and the variables that carry their credentials | `forge.example.com`, `registry.example:5000`, a role account such as `company-agent`, and a variable named for what it holds, such as `FORGE_READ_TOKEN` |
| `internal-email` | A mail address on an internal domain | An RFC 2606 address: `agent@example.com` |

The fleet's own subnet, hostnames and path prefixes are never written down in
this repository, not even as rules. They live in a private pattern file that CI
receives as the `RELEASABILITY_PATTERNS` secret: one `class<TAB>regex` line per
rule, with an optional `/i` suffix on the class for a case-insensitive rule.
The CI step "Releasability (private patterns)" greps the tracked tree with it,
and `pkg/labstrings` reads the same variable when it is set. Without it,
`TestTheTrackedTreeIsPubliclyReleasable` skips and says so.

Local configuration — the burn-in node's node config, its project bindings, its
deployment tooling — belongs in the private `automatethethings` infra
repository, not here.

Benchmark provenance names the hardware class, not the machine: "measured on
an Intel Arrow Lake workstation", never the fleet hostname.

One thing is explicitly *not* a violation:

- **RFC 1918 literals that are not the fleet subnet.** Tests across this tree
  use `192.168.1.x` as an opaque address that is never dialed. The gate does not
  look at `192.168.0.0/16` at large, only at the fleet's own /24.

Test files are published and scanned like everything else; they use neutral
fixtures (`agent-NN`, `gpu-host`, `198.51.100.x`, `/home/agent`). The gate's
own tests (`pkg/labstrings/*_test.go`) carry no fleet values either: they build
their rules from reference values (`203.0.113.x`, `fleet-node-NN`,
`/home/example-owner`).

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
`TestTheTrackedTreeIsPubliclyReleasable`, so `make test` runs it whenever
`RELEASABILITY_PATTERNS` is set — there is no target to remember and no target
list it can fall off. Without the variable the test skips, and the CI step
"Releasability (private patterns)" is the gate. Resolution is textual, so
it works on a tree that does not compile and covers Markdown, YAML, Dockerfiles,
shell, Python and minified JavaScript as readily as Go.

```
RELEASABILITY_PATTERNS="$(cat <private pattern file>)" go test ./pkg/labstrings/   # the gate, ~1s
```

A failure names the file, the line, the class and what to use instead, never
the matched text: a CI log is public, so look at the named line in your own
checkout to see the value.

### Accepting a line

A waiver is for a line whose purpose is the value itself, such as the gate's
own patterns. A line that merely mentions a fleet value is rewritten instead.

Preferred — an **inline waiver**, in the file's own comment syntax, on the
offending line or the line immediately above it:

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

One line in the private pattern file, `class<TAB>regex`, and an update of the
`RELEASABILITY_PATTERNS` secret. Nothing in this repository changes. The
package works out each rule's sieve literal from the regexp itself, and a rule
with no literal every match must contain is run against every file, so a new
rule never blinds itself.

## Before tagging: assemble the changelog

Pull requests add `changelog.d/<issue>-<slug>.md` fragments and never edit
`CHANGELOG.md`. Before tagging, run `make changelog VERSION=vX.Y.Z`, which folds
every fragment into a new `## [X.Y.Z] - <date>` section and empties
`changelog.d`, and commit the result; the tag goes on that commit.
