# Service Registry — CLI

> **Status: NORMATIVE** · Last verified: 2026-10-01
> Every verb and flag, with runnable examples. Feature folder: `docs/service-registry/`.

`quicbaas registry` routes through the active `--transport` (rest, grpc, quic,
mcp, unix, embedded) and acts on the authenticated tenant. `--output` selects
`table` (default), `json` or `yaml`.

| Command | Flags / arguments |
|---|---|
| `registry register` | `--service` `--id` `--address` (required), `--ttl` seconds (default 15), `--meta key=value` (repeatable) |
| `registry renew <leaseID>` | — |
| `registry deregister <leaseID>` | — |
| `registry resolve <service>` | — |
| `registry watch <service>` | `--from-revision N` (0: changes from now), `--max-events N` (exit after N changes; 0 runs until interrupted) |

```bash
quicbaas registry register --service api --id node-a --address 10.0.4.7:8443 --ttl 15 --meta zone=z1
quicbaas registry renew 42
quicbaas registry resolve api --output json
quicbaas registry watch api
quicbaas registry watch api --from-revision 118 --max-events 1 --output json
quicbaas registry deregister 42
```

`watch` prints one line per change (`revision  TYPE  id  address`), or one JSON
or YAML document per change; it exits non-zero when the server ends the watch
(for example a compacted start revision).
