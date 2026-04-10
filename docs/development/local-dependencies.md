# Local Dependencies

QuicBaaS uses `replace` directives in `go.mod` to reference local copies of core dependencies stored in the `.deps/` directory. This enables co-development of tightly coupled libraries without publishing intermediate versions.

## Directory Structure

```
.deps/
  go-quicraft/    # Raft consensus library
  go-qrdb/        # QRDB distributed database engine
  go-xkms/        # Key management system library
  go-signal/       # Signal handling library
```

## Setup

The `.deps/` directory must be populated before building:

```bash
# Clone all dependencies into .deps/
mkdir -p .deps
git clone https://github.com/jeremyhahn/go-quicraft.git .deps/go-quicraft
git clone https://github.com/jeremyhahn/go-qrdb.git .deps/go-qrdb
git clone https://github.com/jeremyhahn/go-xkms.git .deps/go-xkms
git clone https://github.com/jeremyhahn/go-signal.git .deps/go-signal
```

## Replace Directives

The following `replace` directives in `go.mod` map module paths to local directories:

| Module | Local Path |
|--------|-----------|
| `github.com/jeremyhahn/go-quicraft` | `.deps/go-quicraft` |
| `github.com/jeremyhahn/go-qrdb` | `.deps/go-qrdb` |
| `github.com/jeremyhahn/go-qrdb/sdk/go` | `.deps/go-qrdb/sdk/go` |
| `github.com/jeremyhahn/go-xkms` | `.deps/go-xkms` |
| `github.com/jeremyhahn/go-xkms/sdk/go` | `.deps/go-xkms/sdk/go` |
| `github.com/jeremyhahn/go-signal` | `.deps/go-signal` |
| `github.com/jeremyhahn/go-quicbaas/sdk/go` | `./sdk/go` |

## Pinned Dependencies

| Module | Pinned Version | Reason |
|--------|---------------|--------|
| `github.com/armon/go-metrics` | `v0.4.1` | Prevents upgrade to versions with breaking API changes that affect `hashicorp/memberlist` |

## Docker Builds

The `Makefile` automatically stages `.deps/` contents into the Docker build context. No additional setup is required for containerized builds.

## Disabling Local Dependencies

To build against published module versions instead of local copies, remove the `replace` directives from `go.mod` and run `go mod tidy`. This requires that all dependencies are published at compatible versions.
