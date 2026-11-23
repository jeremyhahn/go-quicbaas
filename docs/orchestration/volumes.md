# Volumes

Persistent block volumes for containers, supplied by
[go-blockstore](https://github.com/jeremyhahn/go-blockstore) over its
`VolumeProvider` surface: create, grow-only resize, attach to a device path,
and promote/demote for replicated volumes. SPEC row `orchestration-volumes`
(#1683). go-blockstore supplies the volumes and adds no NBD code; go-quicbaas
adds no NBD server.

## Declaring volumes

A container declares its volumes in `ContainerSpec.Volumes`, one
`VolumeClaim` each (`pkg/orchestration/volumes.go`):

| Field | Meaning |
|-------|---------|
| `name` | The volume's name. A relaunch finds the volume by name and reattaches it. |
| `size_bytes` | Usable capacity. A smaller existing volume is grown; a larger one is left alone (resize never shrinks). |
| `fs_type` | `ext4` or `xfs`: a filesystem volume, bind-mounted at `target`. Empty: a raw block device. |
| `target` | The absolute path in the container: the mount point (required with `fs_type`) or the device node's path (optional for a raw volume; empty keeps the host path). |
| `readonly` | Mount or pass the device read-only. |
| `replicas` | Passed to the provider; zero means its natural count. |
| `labels` | Passed to the provider as the volume's labels. |

```json
{"volumes": [
  {"name": "pgdata", "size_bytes": 10737418240, "fs_type": "ext4", "target": "/var/lib/postgresql/data"}
]}
```

Service manifests (SPEC row `services`) map onto the same claims once that row
defines the manifest.

## Launch

The fleet node agent enables volumes with a `volumes` block in its config
(`pkg/fleet/volumes.go`):

```yaml
volumes:
  provider: local          # drbd is refused: launches do not promote yet
  settings: {}             # the provider's snake_case settings
  node_id: ""              # default: settings.node_id, then the host name; given to the local provider too
  staging_dir: ""          # default: <work_dir>/volumes; the provider name is appended
  lock_dir: ""             # default: /run/lock/quicbaas/volumes; must be the same for every daemon on the host
```

`fleet serve` wraps the node's container runtime in `VolumeRuntime`, the
outermost wrapper. Before a container starts it provisions or grows each
claim, attaches it, mounts filesystem volumes under the staging directory and
rewrites the spec with the resulting bind mounts and devices. The measured
launch digest is taken over that resolved spec. Volumes are released in reverse
order when the container stops, is removed or fails to start, and a failed
attach rolls back the claims already attached. A node without a volume
provider refuses any spec that still carries claims.

The wrapper keeps the wrapped runtime's capabilities (image listing, the
orphan sweep, the reaper, trust tiers): callers find them through
`RuntimeAs`, and the reaper still removes through the wrapper. Its teardown
releases volumes even when the container is already gone (an interactive
container runs with `--rm`) and keeps whatever failed to release for the next
Stop or Remove. A release that fails after the container is gone (removed, by
the agent or by the reaper) moves to the retry list instead, which each reaper
pass and each volume launch retries. A rollback runs on a bounded context of its own, so a
cancelled launch still releases; what it cannot release is retried before the
next launch. After mounting a writable claim on an existing volume, the agent
re-runs Resize at the volume's size, so a filesystem grow left undone while
unmounted (XFS grows only mounted) completes. A volume the provider will not
grow while detached is grown right after Attach, before it is mounted, and a
refused grow rolls the attach back. A `node_id` that differs from
`settings.node_id` is refused at startup.

A volume has one owner on a node. A launch whose claim names a volume another
container holds is refused with `ErrVolumeInUse` before anything is attached.
The owner is recorded under both the container's name and its ID, so the
reaper, which lists full IDs, releases a container launched by name. Ownership
is also written to `.quicbaas-attachments.json` in the staging directory; after
a restart the agent holds those volumes until its startup orphan sweep has
removed the old containers, then releases them. Each attachment is written to
the state file before its Attach and again after it, so a node killed part way
through a launch recovers what it attached; a recovered volume that was never
attached counts as released when Detach reports it not attached. The state file lives in the
staging directory, which is scoped to the daemon's work dir and provider, and
one volume runtime holds it exclusively (a lock on
`.quicbaas-attachments.lock`): a second daemon given the same `staging_dir` is
refused with `ErrVolumeStateLocked` at startup. Each owned volume also holds a
host-wide lock, `<lock_dir>/<provider>-<hash of the provider settings, node_id
excluded>/<claim>.lock`, from its reservation until its release, so two
daemons with their own work dirs over the same backing storage never own one
volume at once: a launch whose volume another daemon holds is refused with
`ErrVolumeInUse`, and so is a restart that recovers one. The agent refuses a
volume launch whose `quicbaas.owner` label is not its work dir
(`ErrVolumeOwnerLabel`), so every volume container is one the startup sweep
removes before the recovered volumes are released.

Spawn requests are not volume launches: a sandboxed spawn whose
`container_config` asks for a privilege class, a device or a volume is refused,
and the sandbox policy clears the class and devices it is given.

## Raw devices and the broker class

A raw claim reaches the container as a device (`--device`), and only a spec in
the broker privilege class (`privilege_class: broker`) may carry devices. Any
other spec's raw claim is refused with `ErrDevicePassthroughDenied` before
anything is provisioned. The class and its devices are part of the measured
digest.

## Replicated volumes

go-blockstore's HA controller (`NewVolumeHAController`) elects each replicated
volume's primary through a `VolumeCoordinator`:

- `QRDBVolumeCoordinator`, over a raft-mode go-qrdb. Its leases are cross-node
  ownership arbitrated by a quorum, which a replicated pair managed from both
  of its nodes needs.
- `LocalVolumeCoordinator`, in-process. Its arbitration stops at the process
  boundary, so it is safe only where one process manages the volume.

A standalone go-qrdb keeps no leases; use the local coordinator there.

The fleet node agent does not start an HA controller, and its launches do not
promote or demote a volume, so it refuses the `drbd` provider at startup
(`ErrVolumeUnsupported`).

## The NBD volume store and diskless boot

The boot node (go-trusted-platform) runs on go-quicbaas as a broker-class
service. It declares the root and data volumes of the diskless nodes it serves
as raw claims, receives them as devices and exports them over NBD. A diskless
node boots over the network from the boot node and attaches its root and data
volumes from that NBD export.

go-trusted-platform's NBD export consumes these types through `sdk/go`
(`sdk/go/orchestration`): the claim, attacher, provider, device, coordinator
and HA controller types are aliases of the server's, so the two never drift.

## See also

- [Architecture](architecture.md): Container Trust and the measured launch.
- [Service plane](../service-plane/architecture.md): the broker privilege class.
