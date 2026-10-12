### Security

- **The forge token no longer reaches a host's disk (go-quicbaas#899).**
  Previously it was written beside the workspace spec under the executing
  host's projects root and bind-mounted into the container. The spec
  directory now holds only `workspace.json`, and a spec that names a token
  file is refused. The daemon delivers the token per session, over exec
  stdin, to `/run/quicbaas/credentials/forge-token`: mode `0400` on a 1 MiB
  tmpfs, owned by the container's user. It is removed on every session
  close, and a failed removal is logged and audited as `forge_credential`
  with no secret in it. The token goes with the container on reap. Anything
  in a worker image that read `/run/quicbaas/workspace/forge-token` should
  follow the spec's `forge_token_file`, which now names the new path.
  Per-job, expiring forge tokens are not yet possible; that needs a
  forge-side capability.
