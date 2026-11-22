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
  provider: local          # or drbd in a build with the drbd tag
  settings: {}             # the provider's snake_case settings
  node_id: ""              # default: settings.node_id, then the host name; given to the local provider too
  staging_dir: ""          # default: DefaultVolumeStagingDir
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
Stop or Remove. A rollback runs on a bounded context of its own, so a
cancelled launch still releases; what it cannot release is retried before the
next launch. After mounting a writable claim on an existing volume, the agent
re-runs Resize at the volume's size, so a filesystem grow left undone while
unmounted (XFS grows only mounted) completes. A `node_id` that differs from
`settings.node_id` is refused at startup.

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
