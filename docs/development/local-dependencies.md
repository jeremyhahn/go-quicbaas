# Local Dependencies

QuicBaaS develops co-evolving sibling repos (go-qrdb, go-xkms, go-signal, etc.)
in a shared Go workspace. There is no `replace` directive boilerplate in any
individual `go.mod` — Go's workspace mechanism (`go.work`) does the resolution.

## Layout

Sibling repos are checked out alongside `go-quicbaas` under a common parent
directory. The parent contains `go.work`:

```
/<workspace-root>/
  go.work          # Go workspace file
  go-quicbaas/     # this project
  go-qrdb/         # QRDB distributed database engine
  go-xkms/         # Key management system
  go-quicraft/     # Raft consensus library
  go-signal/       # Signal CLI bridge
  go-libsignal/    # Libsignal protocol primitives (used by pkg/signalbridge)
  go-objstore/     # Object storage abstractions
  ...              # other workspace modules (see go.work)
```

Run `cat ../go.work` from inside the project to see the full workspace shape.

## Local development

`go build`, `go test`, `gopls`, etc. all work directly because they discover
`../go.work` automatically. No setup required beyond having the sibling repos
checked out alongside this one.

## Docker integration tests

`make integration-test` builds containers that need the same workspace shape
inside Docker. This is handled by **Docker BuildKit's `additional_contexts`** —
the compose file declares a named context `workspace` pointing at the
workspace-root parent directory:

```yaml
build:
  context: ../..              # project root (default context)
  additional_contexts:
    workspace: ../../..       # workspace root (parent of project)
  dockerfile: test/integration/Dockerfile.node
```

Inside `Dockerfile.node`, sibling repos are pulled selectively from the
`workspace` named context:

```dockerfile
COPY --from=workspace go-libsignal  ./go-libsignal
COPY --from=workspace go-objstore   ./go-objstore
COPY --from=workspace go-qrdb       ./go-qrdb
COPY --from=workspace go-quicraft   ./go-quicraft
COPY --from=workspace go-signal     ./go-signal
COPY --from=workspace go-xkms       ./go-xkms
COPY . ./go-quicbaas                # this project (default context)
```

A pruned `go.work` is then written inside the image (the host `go.work`
references unrelated 21GB Layer 0 modules like `go-trusted-platform` that we
do not ship to Docker). The pruned file lists only the modules actually
copied:

```dockerfile
RUN { \
        echo 'go 1.26.3'; \
        echo 'use ('; \
        echo '    ./go-libsignal'; \
        echo '    ./go-objstore'; \
        ... \
        echo ')'; \
    } > go.work
```

`go build` and `go test` inside the container then resolve everything via this
go.work — no Docker-specific `replace` rewrites in any individual `go.mod`,
no `.deps/` staging directory.

### Why no `.deps/` directory anymore

Earlier versions of this project staged sibling repos via `rsync` into
`.deps/` and ran a chain of `sed` commands inside the Dockerfile to rewrite
`go.mod` `replace` directives to `=> /deps/<sibling>`. The migration to
parent-`go.work`-based local development made those `replace` directives
unnecessary, which broke the Dockerfile chain. Switching Docker to
`additional_contexts` brings the build environment in line with how local
dev already works.

## Pinned Dependencies

| Module | Pinned Version | Reason |
|--------|---------------|--------|
| `github.com/armon/go-metrics` | `v0.4.1` | Prevents upgrade to versions with breaking API changes that affect `hashicorp/memberlist` |

## Disabling local resolution

To build against published module versions instead of local workspace copies,
remove or rename the parent `go.work` (e.g. `go.work.off`) and run
`go mod tidy`. This requires that all sibling dependencies are published at
compatible versions on the module proxy.
