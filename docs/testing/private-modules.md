# Private modules in the Docker test builds

> **Status: REFERENCE** · Last verified: 2026-11-09
> How the integration, seal and e2e images resolve their modules.

The integration, seal and e2e images run `go mod download` during their build,
against the module proxy and the checksum database. Every module resolves from
there, so no module needs a credential and none reaches an image build.

## The gomodauth secret file

The compose files hand BuildKit a `gomodauth` secret, and the Dockerfiles mount
it for their module download. Both keep that secret mount and its path, so the
file must exist when a build starts.

`make stage-gomodauth` creates it at `$(GOMODAUTH_ENV)` (default
`/tmp/quicbaas-gomodauth.env`, outside the build context), empty and 0600. An
existing file is emptied and chmodded to 0600 rather than reused as it was.
Nothing is written into it, whatever credential the environment holds.

A file CI hands in through `MODULE_ENV_FILE` belongs to the caller: no target
stages into it or empties it, so it is left as it was handed over.

Override (make variable or environment):

- `GOMODAUTH_ENV` -- where the secret file is staged. Set a distinct path per
  run when two runs share a host, since each run empties its file when its
  build finishes.

## Which targets stage it

Every target whose compose file mounts the secret: `integration-test` (and so
every `integration-test-<pkg>`), `integration-test-seal`,
`integration-test-tenantbarrier`, `integration-bench`, `test-e2e-docker` and
`test-e2e-acp-docker`. Each empties the file once its images are built, and
from an EXIT trap armed before the build starts, so a failed build, a Ctrl-C or
a cancelled job empties it too. `make clear-gomodauth` empties it by hand (and
creates it 0600 when it is absent).

Every image those targets build is handed the secret by its compose file and
mounts it for its module download -- including the seal lane's
`Dockerfile.seal`, which builds its siblings from the `workspace` context and
fetches every other module through the proxy.

## The gate

`make verify-gomodauth` pins this contract against a faked Docker:

- no credential reaches an image build, whatever the environment holds: the
  file stays empty, and nothing reaches the log or a docker command line;
- the file mode, 0600, including over an existing world-readable file;
- each target's build sees an empty file and leaves it empty, also when it is
  interrupted mid-build;
- every image built from the staged compose files mounts the secret, so each
  keeps a path to the file.

It drives `integration-test` with `GOMOD_BACKUP`/`SDK_GOMOD_BACKUP` pointed
inside its own workspace, so a real run's go.mod backups are never touched.
