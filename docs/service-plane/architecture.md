# Trusted service plane: architecture

> **Status: NORMATIVE** · Last verified: 2026-10-15
> The design for the SPEC rows `container-runtime`, `services`, `oci-registry`,
> `measured-launch` and `service-policy`. All five are planned; the parts
> already built are noted where they appear.

The trusted service plane runs signed, measured services on fleet nodes. It
has one container runtime, one service manifest with lifecycle verbs, one
rollout controller, one OCI registry, measured launch with default deny, and
one policy engine.

Code that another layer owns today moves here with its history and tests,
through a path-filtered history merge, and is wired here before the copy it
replaces is removed. Code that belongs to a lower layer moves there and is
consumed through `sdk/go`. Package names are this repo's:
`pkg/containerruntime`, `pkg/ociregistry`, and the Verifier in `pkg/service`.

## Container runtime

The row `container-runtime` provides one container runtime,
`pkg/containerruntime`. It is moved in from go-trusted-platform
`pkg/compute/podman`. The parts already built are described in
[agent-fleet: Container runtime](../agent-fleet/architecture.md#container-runtime).

**The exec core is the one rewrite.** The `crun`, `podman` and `skopeo` calls
are replaced by three in-process libraries:

- `containers/image` for pull, copy and the signature hooks;
- `containers/storage` for layer unpack;
- runc `libcontainer` for create, start, kill and delete.

The rest of the package moves unchanged. It implements
`orchestration.ContainerRuntime` and that interface's optional interfaces.

**Contract tests.** The rewritten core must pass the following tests. Only the
argv-building tests leave with the old exec core: `TestExecContainerRuntime_BuildRunArgs*`,
`TestBuildExecArgs`, `TestGPUFlag` and `TestExecContainerRuntime_AutoDetect`.

- From go-trusted-platform:
  - `manager_test.go`;
  - `attach_test.go`, `exec_test.go`, `attestation_test.go`, `metrics_test.go`,
    `types_test.go`, `config_test.go` and `errors_test.go`;
  - from `runtime_test.go`: `TestRuntimeConfig_Validate`, `TestParseUserSpec`,
    `TestOCIConfigStructs`, `TestOCIResources`, `TestOCICapabilities` and
    `TestOCINamespace`;
  - `TestRuntime_TPMDeviceComesOnlyFromTheContainerConfiguration` and
    `TestRuntime_ResourceLimitsReachTheContainer`.
- From this repo:
  - `TestExecContainerRuntime_{Run,Stop,Remove,Logs,IsRunning}Validation`;
  - `TestContainerSpec_Validation` and `TestContainerMount_Validate`;
  - `TestExecContainerRuntime_VerifyPinnedImage` and
    `TestExecContainerRuntime_RunRejectsUnpinnedAtPinnedLevel`;
  - `TestExecContainerRuntime_Exec_*` and `TestExecContainerRuntime_RunInteractive_*`;
  - `TestSweepOwned_*`, `TestInspectImage_*` and `TestRunImage_*`.

Once these tests pass, two pieces are removed:

- `ExecContainerRuntime`: `pkg/orchestration/container_runtime*.go`,
  `container_exec.go`, `container_sweep.go`, `container_reaper.go`, and the
  shell-outs in `container_image.go`;
- the docker/podman selector in `pkg/orchestration/fleet/container_runtime.go`.

**Where it runs.** The runtime lives in the fleet node agent. The agent is
built with cgo for libcontainer's `nsenter` init step. quicbaasd stays
`CGO_ENABLED=0` and drives the agent over the platform transports with mTLS.
The Go-native FIPS 140-3 module is pure Go, so the agent's cgo build does not
affect it: a `CGO_ENABLED=1 GOFIPS140=latest` binary still reports
`fips140.Enabled()`. `nsenter` links no crypto.

### Co-location

If jobs run on a host that also runs quicbaasd, a fleet node agent runs on
that host as a separate process. This includes a single-node deployment.

- **Enrollment.** The agent enrolls against the local server like any other
  node:
  - `server_addr` is `https://localhost:<rest-port>`;
  - `server_ca_file` is the local CA;
  - the node certificate comes from that CA.

  The server certificate already carries the loopback names
  (`pkg/server/server_cert_sans.go`). A loopback `bind_addr` is registered as
  dialable.
- **No shared container runtime in quicbaasd.** The embedded fleet node runs
  inside quicbaasd, so it cannot link libcontainer. On the server's host,
  container jobs go to the co-located agent. The embedded node keeps only
  backends that launch no container.
- **Builds.** The agent needs a linux build target with `CGO_ENABLED=1`, plus a
  twin with `GOFIPS140=latest`. That build is the `fleet deploy` default.
- **Local install and bind address.**
  - A co-located agent installs locally with the same unit and start script,
    and needs no sshd.
  - Its default bind address is the advertised host, never the wildcard.
- **Work-dir lock.** The owner label on containers is the work dir, and each
  startup sweep (`SweepOwned`) removes every container that carries its label.
  Each runtime therefore takes an exclusive lock on its work dir at startup. A
  process that cannot take the lock refuses to start.
- **Clusters.** In a cluster, only that node's own server can dial a loopback
  registration. A co-located agent in a cluster therefore sets a routable
  `advertise_addr`, and enrollment checks it with the reachability probe.
- **Ports.**
  - The agent's default port, 9091, does not collide with the server's REST,
    QUIC, gRPC or raft ports.
  - It does collide with the dev cluster's second node gRPC port
    (`pkg/config/devcluster.go`). One of those two defaults moves.

## Services and lifecycle verbs

The row `services` makes a `Service` a signed manifest, stored in go-qrdb,
scoped to a tenant, with CAS-arbitrated writes. The manifest holds:

- the image digest and provenance reference;
- the sandbox, mount and network policy references;
- resources, volumes and ports;
- the built-in role the service runs under;
- the registered name;
- placement constraints.

**Verbs.** `deploy`, `undeploy`, `redeploy`, `start`, `stop`, `resume` and
`restart` live in one `ServicePlane` service in `pkg/service`. They are served
on REST, gRPC, QUIC, MCP, Unix and embedded, and in the SDK as
`SDK().Compute().Services()`.

**On deploy, the platform grants the service:**

- a workload identity: an x509-svid from go-xkms,
  `spiffe://<tenant>/...`, where the tenant is the SPIFFE trust domain;
- a certificate through the re-exported go-xkms ACME;
- a name in `pkg/registry`;
- RBAC grants from the built-in role;
- secrets bound to the identity.

**Placement** reuses `pkg/scheduler/placement`. `FleetDeployService`, the SSH
install of the daemon itself, stays as node bootstrap. Its verbs are not
service verbs.

### Releases, slots and rollout

One `Release` object carries the image, manifest, policy, reference values and
provenance, all signed together. It serves both service redeploys and
go-trusted-platform's staged node upgrades.

Every service has two slots, A and B. A redeploy fills the inactive slot,
appraises it, then switches the registry name. The previous slot is kept for
an immediate rollback.

Exactly one rollout controller exists. It is leader-elected through the
scheduler's leader election and drives one interface:

```go
type Strategy interface {
    Plan(current, target Release) (Rollout, error)
    Step(ctx context.Context, r *Rollout) (Phase, error)
    Rollback(ctx context.Context, r *Rollout) error
}
```

`ABSwitch` is the first strategy. `BlueGreen` and `Canary` are later
strategies on the same interface; there is never a second controller.

### Boot node, NBD and the deployer

go-trusted-platform owns provisioning and netboot. It delivers its boot node,
including the NBD server, as a signed `Release`.

This repo hosts that release as a service in the **broker** privilege class.
The broker class grants:

- host network;
- the raw DHCP and TFTP sockets;
- the NBD port;
- block-device access to the attached go-blockstore volumes.

The boot node also gets what every service gets: the lifecycle verbs, A/B
slots, measurement and transparency-log records.

go-trusted-platform's `pkg/bootstrap/deployer` moves to `pkg/orchestration/fleet/deploy`,
beside `FleetDeployService`. It keeps:

- the `Deployer` interface;
- SSH install and netboot-media streaming;
- `DeployConfig` and `DeployResult`;
- its tests.

Its local `docker run` and `docker load` calls are repointed to the service
plane's `deploy` verb. The docker default and the docker/podman runtime field
are removed.

## OCI registry

The row `oci-registry` moves go-trusted-platform's `pkg/registry` into
`pkg/ociregistry`, together with its distribution handler, its gRPC adapter
and its tests. The registry is served at `/v2/` and in the SDK as
`Compute().Images()`. The move keeps:

- the distribution routes;
- the upload limits;
- manifest handling;
- the auth handler.

- **Storage.** Blobs and metadata go through the go-quicbaas object-storage
  service, which re-exports go-objstore. go-objstore owns all object storage;
  go-qrdb owns nothing there.
- **Layer encryption** uses go-qrdb DEKs. The file-backed `LocalKMSClient` is
  removed.
- **Referrers API.** The registry serves the referrers API, which provenance
  depends on.
- **Authentication.** Two methods are wired:
  - mTLS workload identity through the go-qrdb auth chain;
  - TPM attestation through go-xkms peergate, re-exported.

  `RequireAttestation` is on by default.

## Measured launch

The row `measured-launch` moves go-trusted-platform's `pkg/compute/container`
into `pkg/orchestration`. The move keeps:

- the measurement log and its hash chain;
- `FleetContainerAttestor`;
- IAK binding.

Its quote checks call the Verifier's `AppraiseQuote`
(`pkg/service/attestation_verifier.go`), which calls go-xkms evidence
primitives. Launch-time PCR checks use `AppraiseQuote` against the
reference-values store. There is no per-spec PCR list.

Two more go-trusted-platform pieces move in:

- **Golden values** (the profile baseline, the TPM golden PCRs, the golden
  values, and the registry's required PCRs) move into the reference-values
  store as signed CoRIM reference values, with a one-time import.
  go-trusted-platform reads them back through `SDK().Security()`.
- **The attestation store and reporting cache** move into the attestation
  results in go-qrdb (`NewQRDBAttestationStore`).

The raw-TPM evidence checks move to go-xkms, not here.

**Measurement.** Before a container starts, the runtime builds its launch
measurement with `LaunchMeasurement`, from the image digest, config hash and
policy hash. It appends the measurement to the node measurement log and calls
`HostTPM.ExtendPCR` on the application PCR, which is PCR 16 by default. If the
extend fails, the container does not start. The Verifier replays the log
against the quoted PCR.

`HostTPM` (`Quote`, `ReadPCRs`, `ExtendPCR`) is the contract between the two
repos. It is defined in `pkg/orchestration` and re-exported through `sdk/go`.
go-trusted-platform implements it on its nodes and supplies it to the fleet
agent at startup.

**Cluster manifest.** A signed list of every service release in the cluster,
held in go-qrdb.

**Provenance.** The software factory (`pkg/orchestration/sdlc`) emits SLSA provenance. It is
signed through go-xkms image signing and stored as a DSSE referrer.

**Default deny.** A digest runs only if it is signed, carries provenance and is
listed in the reference values. Each tenant or service may have an audited
`allow-unsigned` policy. That policy is extended into the measurement log, so
it appears in the node's evidence.

## Service policy

The row `service-policy` moves the Go-coded policies into one engine. The
policies come from `pkg/orchestration` and `pkg/orchestration/fleet/sandbox_policy.go`:
admission, sandbox, sandbox-network, mount, trust tiers, communication, role,
lifecycle and spend.

The engine dispatches on the policy kind through a map. Each kind's evaluator
is its existing Go code and tests, moved rather than rewritten.

- **Policy documents** are data. They are signed with the tenant key through
  go-xkms, stored in go-qrdb, and verified on load.
- **go-trusted-platform's rego rules** map onto the existing kinds.
- **IAM authorization** stays with go-qrdb's PDP. The policy engine is never a
  second engine for the same decision.

## Transparency-log records

The plane appends these records to go-xkms's transparency log through the
re-exported append API (row `transparency-log`):

- admission decisions;
- deployments and slot switches;
- policy changes;
- cluster-manifest versions.

Appends fail closed: if no append receipt comes back, the slot does not
switch. `pkg/security` `AuditLogger` remains the operational audit.

## Rejected alternatives

| Alternative | Why rejected |
|---|---|
| Keep spawning `crun`, `podman` and `skopeo` | The runtime depends on host binaries, and today's rootfs is a placeholder because the image is never unpacked |
| libpod in-process | libpod still launches `conmon` and an OCI runtime binary |
| The runtime inside quicbaasd | quicbaasd would need cgo; the agent carries the cgo build instead |
| A second rollout controller per strategy | Strategies share one controller through `Strategy` |
| Rewriting the policy engine in rego | Moving the Go evaluators keeps their tests; rego rules map onto the kinds |
| File-backed registry storage and keys | They form a second datastore beside go-qrdb |

## What the plane does not do

- It implements no log, Merkle tree or proof code; that comes from go-qrdb and
  go-xkms.
- It does no image signing or evidence checking of its own; go-xkms provides
  both.
- It does not make authorization decisions; go-qrdb does.
