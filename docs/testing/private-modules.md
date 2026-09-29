# Private modules in the Docker test builds

The integration, seal and e2e images run `go mod download` during their build.
Private first-party modules -- today `github.com/jeremyhahn/go-auth` -- are
served by the Gitea origin, not by `proxy.golang.org` or GitHub, so the build
needs the dependency-resolution.md section-6 variables (`GOPRIVATE` and a
`GIT_CONFIG_*` `insteadOf` rewrite carrying a read token). Without them the
build asks the public proxy, then GitHub, and fails with
`unknown revision v0.2.0-alpha`.

## How the credentials reach the build

`make stage-gomodauth` writes the four variables to `$(GOMODAUTH_ENV)`
(default `/tmp/quicbaas-gomodauth.env`, outside the build context). The file
is chmodded to 0600 before a token goes in, so an existing file created
world-readable is tightened rather than reused as it was.
The compose files hand that file to BuildKit as the `gomodauth` secret; the
Dockerfiles source it for the single `RUN go mod download`. The token never
appears in a build arg, a layer, image metadata or the log.

The first source that has a value wins:

| Source | Where it applies |
|---|---|
| `GIT_CONFIG_KEY_0` / `GOPRIVATE` / `GIT_CONFIG_*` in the environment | Gitea Actions runners inject these; staged as-is. |
| `GITEA_READ_TOKEN` in the environment | Any host. A `read:repository` token. |
| `$(GOMODAUTH_TOKEN_KEY)` in `$(GOMODAUTH_TOKENS_FILE)` | Agent hosts: `AGENT_TOKEN_GO_QUICBAAS` in `~/.agent-identity/tokens.env`. |
| none | The file is empty; modules resolve from the public proxy (GitHub CI). |

A developer host that resolves modules through an SSH `insteadOf` in
`~/.gitconfig` still needs one of the token sources: a container cannot see
that rewrite or the SSH agent.

A token needs the forge it is good for. The tree names no forge, so
`GITEA_HTTP_ORIGIN` -- the Gitea HTTP origin the rewrite points at, as
`http[s]://host:port` -- has no default. It is read from the environment (or
make's command line), else from a `GITEA_HTTP_ORIGIN=` line in
`$(GOMODAUTH_TOKENS_FILE)`. A private deployment keeps it in that file, beside
the token:

```sh
# ~/.agent-identity/tokens.env
AGENT_TOKEN_GO_QUICBAAS=<read token>
GITEA_HTTP_ORIGIN=https://gitea.example.com:3000
```

A token with no origin from either source fails `stage-gomodauth` (and every
target that stages) with a message naming both places; nothing is staged. A
runner's injected `GIT_CONFIG_*` variables already carry the origin, and a host
with no token needs none.

Overrides (make variables or environment):

- `GOMODAUTH_TOKENS_FILE`, `GOMODAUTH_TOKEN_KEY` -- where the token is read from.
- `GOMODAUTH_ENV` -- where the secret file is staged. Set a distinct path per
  run when two runs share a host, since each run empties its file when its
  build finishes.

A value that is not token-shaped (`[A-Za-z0-9_-]`) is refused and nothing is
staged; the token itself is never printed.

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
`Dockerfile.seal`, which builds its siblings from the `workspace` context but
fetches every other module (go-auth among them) behind the secret and copies
that module cache into its runtime stage.

`make verify-gomodauth` pins all of this against a faked Docker: the staged
content for each source, the file mode (including over an existing 0644 file
and after `clear-gomodauth` under umask 022), that the token reaches neither
the log nor a docker command line, that each target builds with the file
populated and leaves it empty -- also when interrupted mid-build -- and that
every image built from the staged compose files is handed the secret and reads
it. It drives `integration-test` with `GOMOD_BACKUP`/`SDK_GOMOD_BACKUP` pointed
inside its own workspace, so a real run's go.mod backups are never touched.

## Checking a built image

After a build on a developer host, confirm the token is in no image by
searching for a fragment of it you already hold, without printing it:

```sh
for img in $(docker compose -f test/integration/docker-compose.integration.yml config --images); do
  docker history --no-trunc "$img" | grep -c "$FRAGMENT"            # expect 0
  docker save "$img" | grep -ac "$FRAGMENT"                        # expect 0
done
```

`docker save` emits layers uncompressed with the classic image store; with the
containerd store, extract and decompress each layer blob before searching.
