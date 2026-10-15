# go-quicbaas: trusted service plane design (layer 4)

Status: approved with corrections by the Company Agent, 2026-10-15 00:25Z. This revision carries the corrections and the owner rulings of CA 00:32Z. It implements company plans/trusted-service-plane.md (owner-approved). The SPEC rows follow this page.

**Rulings (CA 00:25Z):**
- The policy engine stays ours, in Go, moved and not rewritten (§6).
- Destination package names are ours as the destination owner, and gtp adopts them: `pkg/containerruntime`, `pkg/ociregistry`, and the Verifier in `pkg/service`.
- Owner (CA 00:32Z): libcontainer in-process, with no binaries and no sockets. The runtime lives in the fleet node agent, built with cgo for its init step, and quicbaasd stays `CGO_ENABLED=0` (§1).
- Owner (CA 00:32Z): the node agent runs on the same host as the server whenever jobs run on that box, a single-node deployment included (§1, co-location).
- Owner (CA 00:32Z): the tenant is the SPIFFE trust domain, a form go-qrdb accepts (§2).

**Routing note.** DS-1737 lists image signing among the moves *in*. The plan's ownership table gives image signing and verification to go-xkms ("signer keys in its KMS, DSSE referrers"). This page follows the plan: gtp `pkg/image` goes to go-xkms, and we consume it.

**Moves.** Every removal is a **move**: the code goes to its owning layer with its history and tests and is wired there; only then is the copy left behind removed (owner rule, CA 23:29Z). Moves into this repo bring the source history through a path-filtered history merge from a scratch clone, never a copy-paste.

**Step 0, a precondition.** Main pins go-xkms v0.49.0-alpha and go-qrdb v0.30.1-alpha. The current tags are v0.54.0-alpha and v0.48.0-alpha. Bump both first (#1171 carries go-xkms v0.52 and go-qrdb v0.47). Without the bump, peergate, the x509-svid profile and every later go-xkms seam cannot reach the SDK.

## Facts that change the brief

- **The runtime spawns binaries today.** gtp `pkg/compute/podman` (12 files, ~3.8k LOC) is not in-process. It runs `crun` create/start/kill/delete plus `podman pull` and `skopeo copy`, and never unpacks the image (`runtime.go:278` is a placeholder rootfs).
- **libpod still spawns binaries.** Even in-process, libpod launches `conmon` and an OCI runtime binary, which is why the owner ruled for libcontainer (§1).
- **No A/B OTA exists to copy.** gtp has no A/B OTA code. Its nearest model is the luks-root `current`/`next` swap with a confirmed epoch. The slot model in §3 is therefore new here, and gtp's staged upgrades consume it.
- **The gtp registry has no real storage or TPM auth.** It is file-backed (`<PlatformDir>/registry`), its layer encryption uses a file `LocalKMSClient`, `RequireAttestation=false`, and the TPM auth handler is never constructed.
- **Two lower-layer seams are missing.** go-xkms has no transparency log and no DSSE image signing. go-qrdb has no verifiable-log type. go-xkms peergate is shipped. Rows 4, 5 and 7 depend on these seams and build bottom-up.

## 1. One container runtime (row `container-runtime`)

**Moves into this repo:**

| Source | Destination | Kept |
|---|---|---|
| gtp `pkg/compute/podman` + tests | `pkg/containerruntime` | `PodmanManager` lifecycle model, attestor hooks, exec, attach and pty, metrics, and their tests |

**The one rewrite in this design is the exec core of the moved podman package.**
- The `crun`/`podman`/`skopeo` exec calls in `runtime.go` and `manager_ops.go` are replaced by:
  - in-process `containers/image` (pull, copy, signature hooks);
  - `containers/storage` (layer unpack, which fixes the rootfs placeholder);
  - runc `libcontainer` (create, start, kill, delete).
- Everything else in the package moves unchanged.
- The package implements our existing `orchestration.ContainerRuntime` interface and its optional interfaces.

**Contract tests kept, run against the rewritten core.** Only the argv-building tests go with the exec core: `TestExecContainerRuntime_BuildRunArgs*`, `TestBuildExecArgs`, `TestGPUFlag`, `TestExecContainerRuntime_AutoDetect`.
- **From gtp:**
  - `manager_test.go` (`TestPodmanManager_*`, `TestManagerOption_*`);
  - `attach_test.go`, `exec_test.go`, `attestation_test.go`, `metrics_test.go`, `types_test.go`, `config_test.go`, `errors_test.go`;
  - in `runtime_test.go`: `TestRuntimeConfig_Validate`, `TestParseUserSpec`, `TestOCIConfigStructs`, `TestOCIResources`, `TestOCICapabilities`, `TestOCINamespace`;
  - in `runtime_tpm_device_test.go`: `TestRuntime_TPMDeviceComesOnlyFromTheContainerConfiguration`, `TestRuntime_ResourceLimitsReachTheContainer`.
- **From ours:**
  - `TestExecContainerRuntime_{Run,Stop,Remove,Logs,IsRunning}Validation`;
  - `TestContainerSpec_Validation`, `TestContainerMount_Validate`;
  - `TestExecContainerRuntime_VerifyPinnedImage`, `TestExecContainerRuntime_RunRejectsUnpinnedAtPinnedLevel`;
  - the `TestExecContainerRuntime_Exec_*` behaviour tests (exit status, missing or stopped container, env never in argv, credential never logged, context cancel);
  - `TestExecContainerRuntime_RunInteractive_*`;
  - `TestSweepOwned_*`;
  - `TestInspectImage_*` and `TestRunImage_*`.

**Removed after the contract tests pass on the moved runtime:**
- `ExecContainerRuntime`: `pkg/orchestration/container_runtime*.go`, `container_exec.go`, `container_sweep.go`, `container_reaper.go`, and the shell-outs in `container_image.go`.
- The docker/podman selector in `pkg/fleet/container_runtime.go`.

**Where it runs (owner ruling, CA 00:32Z).**
- runc `libcontainer` rather than libpod: no `conmon` or `crun` binary, no socket.
- The runtime lives inside the **fleet node agent** (`pkg/fleet` daemon, the privileged broker of plan §3.3). It is built with cgo for libcontainer's `nsenter` init, and re-executes itself as container init.
- quicbaasd stays `CGO_ENABLED=0` and drives the agent over the platform transports with mTLS.

**FIPS.** The Go-native FIPS 140-3 module is unaffected by the agent's cgo build. Checked on Go 1.27.2: a `CGO_ENABLED=1 GOFIPS140=latest` binary reports `fips140.Enabled() == true` with `DefaultGODEBUG=fips140=on`. The module is pure Go, and `nsenter` links no crypto. `GOEXPERIMENT=boringcrypto` stays unused.

**Co-location (owner ruling, CA 00:32Z).** Co-location is designed. When jobs run on a host that runs quicbaasd, a fleet node agent runs on that host too, as its own process; this includes a single-node deployment.
- The agent enrolls against the local server like any other node. It uses `server_addr` `https://localhost:<rest-port>`, the local CA as `server_ca_file`, and a node certificate from that CA.
- The server's TLS certificate already carries the loopback names (`pkg/server/server_cert_sans.go`).
- Enrollment registers a loopback `bind_addr` (default `127.0.0.1:9091`) unchanged (`dialableFleetNodeHost`), so the local server dials the agent over loopback.
- Nothing in enrollment refuses a loopback server or bind address.
- The default ports do not collide: the agent uses 9091; the server uses REST 8443, QUIC 9443, gRPC 11443 and raft 63000.
- The default owner labels differ, so neither process's sweep matches the other's containers. The owner label is the work dir: `/var/lib/quicbaas-agent` for the agent and `<data_dir>/embedded-agent` for the embedded node.

**What prevents it today, each fixed in the `container-runtime` row's lanes:**
1. **The embedded fleet node cannot carry the runtime.** `FleetNodeKindEmbedded` runs inside quicbaasd, and quicbaasd is `CGO_ENABLED=0`, so it cannot link libcontainer.
   - On the server's host, container jobs go to the co-located agent process.
   - The embedded node keeps only backends that launch no container. Its container runtime is removed with the exec core.
2. **No cgo agent build exists.** `fleet deploy` ships `bin/quicbaas-static`, and every release and FIPS build of the `quicbaas` binary is `CGO_ENABLED=0` (Makefile `build-static-cli`, `build-fips-cli`, `build-static-fips-cli`, the cross builds). The only cgo build is the desktop GUI build.
   - The row adds a linux agent build target, `CGO_ENABLED=1` plus its `GOFIPS140=latest` twin, and makes it the `fleet deploy` default.
3. **`fleet deploy` reaches nodes only over SSH and binds `0.0.0.0:9091` by default.**
   - A co-located agent needs a local install path that needs no sshd: the same unit and start script, written locally.
   - Its default bind must be the advertised host, never the wildcard (the #185 rule).
4. **Nothing stops two runtimes on one host sharing a work dir.** The owner label is the work dir, and each startup sweep (`SweepOwned`) removes every container carrying that label. If an agent's `work_dir` equals the embedded node's `data_dir`, or another agent's, each process removes the other's containers.
   - The fix: an exclusive lock on the work dir at startup. The process that cannot take the lock refuses to start.
5. **In a cluster, a loopback registration only works for that node's own server.** Another node's server cannot dial `127.0.0.1`.
   - A co-located agent in a cluster sets a routable `advertise_addr`, and enrollment checks it with the reachability probe that is already there.
   - Single-node needs nothing more.
   - The dev cluster's second node uses gRPC port 9091 (`pkg/config/devcluster.go`), which collides with the agent's default. The row moves the agent default or the dev port.

## 2. Service manifest and lifecycle verbs (row `services`)

**The manifest.** A `Service` is a signed manifest. Its fields:
- the image digest and provenance reference;
- the sandbox, mount and network policy references;
- resources, volumes and ports;
- the built-in role it runs under;
- the registered name;
- placement constraints.

It is stored in qrdb, tenant-scoped, and its writes are CAS-arbitrated.

**The verbs.** `deploy`, `undeploy`, `redeploy`, `start`, `stop`, `resume` and `restart` live in one `ServicePlane` service in `pkg/service`. They are served on REST, gRPC, QUIC, MCP, Unix and embedded, and in the SDK as `SDK().Compute().Services()`.

**On deploy the platform grants:**
- a workload identity: an x509-svid from go-xkms, `spiffe://<tenant>/...`, with the tenant as the trust domain (owner ruling);
- a certificate through the re-exported go-xkms ACME;
- a name in `pkg/registry` (existing);
- RBAC grants from the built-in role;
- secrets bound to the identity.

**Existing code that is kept:**
- `FleetDeployService` (SSH install of the daemon itself) stays as node bootstrap. Its verbs are not service verbs. gtp's `remote-deploy` row moves it to QUIC later.
- Placement reuses `pkg/scheduler/placement`.

## 3. A/B slots, one rollout controller, one strategy interface (in row `services`)

**The release object.** One `Release` carries the image, manifest, policy, reference values and provenance, all signed together. It serves both service redeploys and gtp's staged node upgrades (seam).

**Slots and rollback.** Every service has slots A and B:
- A redeploy fills the inactive slot, appraises it, then switches the registry name.
- The previous slot is kept for an immediate rollback.

**The controller.** There is exactly one leader-elected rollout controller, using scheduler leader election. It drives `Strategy`:

```go
type Strategy interface {
    Plan(current, target Release) (Rollout, error)
    Step(ctx context.Context, r *Rollout) (Phase, error)
    Rollback(ctx context.Context, r *Rollout) error
}
```

`ABSwitch` ships first. `BlueGreen` and `Canary` are tracked as later strategies on the same interface; there is never a second controller.

## 4. OCI registry, re-homed from gtp (row `oci-registry`)

**Moves into this repo:**

| Source | Destination | Kept |
|---|---|---|
| gtp `pkg/registry` (12 files, ~4.4k LOC), `pkg/webservice/v1/rest/registry/ocidist.go` and the gRPC adapter, with tests | `pkg/ociregistry`, served at `/v2/` and in the SDK as `Compute().Images()` | the distribution routes, upload limits, manifest handling and auth handler |

**Changed inside the moved code:**
- Its storage client gets a qrdb implementation: blobs and metadata go through go-qrdb's storage API, with the objstore backend selected by configuration; there is no direct go-objstore import.
- `LocalKMSClient` is replaced by qrdb DEKs.
- The referrers API is added; provenance needs it.

**Authentication, both wired for real:**
- mTLS workload identity through the qrdb auth chain;
- TPM attestation through go-xkms peergate, re-exported, with `RequireAttestation` on by default.

**Removed after:** the gtp copy, by gtp's lane.

## 5. Measured containers, cluster manifest, provenance, default deny (row `measured-launch`)

**Moves into this repo:**

| Source | Destination | Kept |
|---|---|---|
| gtp `pkg/compute/container` (27 files, ~11.7k LOC, already built over our SDK) with tests | `pkg/orchestration` | the measurement log and its hash chain, `FleetContainerAttestor`, IAK binding |

Its own quote checks call our Verifier (`AppraiseQuote`), which calls go-xkms evidence primitives.

**Workload attestation moves in (the Verifier side only):**

| Source (gtp) | Destination | Kept |
|---|---|---|
| Golden-value sources: profile `PCRBaseline`, `TPMConfig.GoldenPCRs`, `GoldenValues`, registry `requiredPCRs`, the attestation golden-values table, with tests | our reference-values store (`pkg/service/attestation_verifier.go`), as signed CoRIM reference values, with a one-time import | the values and their fixtures; gtp reads them back through `SDK().Security()` |
| The file-backed attestation store and reporting cache | our attestation results in qrdb (`NewQRDBAttestationStore`) | the result schema and its tests |

The evidence checks in `pkg/attestation/vm` (quote, AK and EK on raw go-tpm) go to **go-xkms**, not here. That is go-xkms's move; we consume it through `AppraiseQuote`.

**Measurement.** Image digest, config hash and policy hash go into the node measurement log and the application PCR. PCR 16 is the default (`LaunchMeasurement` exists).

**Cluster manifest.** A signed list of every service release per cluster, held in qrdb.

**Provenance.** The software factory (`pkg/sdlc`) emits SLSA provenance, signed through go-xkms image signing as a DSSE referrer (seam).

**Default deny.** Only signed, provenance-bearing, reference-listed digests run. An audited `allow-unsigned` policy per tenant or service is extended into the measurement log, so it shows in the node's evidence.

## 6. Policy as signed data, one engine (row `service-policy`)

**Today:**
- ~12 Go-coded policies in `pkg/orchestration`: admission, sandbox, sandbox-network, mount, trust tiers, communication, role, lifecycle and spend;
- `pkg/fleet/sandbox_policy.go`;
- gtp's embedded OPA rego.

**Proposal.**
- **Move** these into one `pkg/policy` engine with map dispatch on policy kind. The Go evaluators and their tests are kept as the kinds' evaluators.
- **Policy documents** are data, signed with the tenant key through go-xkms, stored in qrdb, and verified on load.
- **gtp's rego rules** map onto the existing kinds.
- **IAM authorization** stays qrdb's PDP; it is not a second engine for the same decision.

**Ruled (CA 00:25Z):** the engine stays ours, in Go, moved and not rewritten. gtp's rego rules map onto its kinds.

## 7. Our records in the go-xkms transparency log (row `transparency-log`, #1341, PR #1342)

**What we append:**
- admission decisions;
- deployments and slot switches;
- policy changes;
- cluster-manifest versions.

**How.** Appends go through the re-exported go-xkms append API and are fail-closed: no append receipt, no switch. `pkg/security` `AuditLogger` stays as the operational audit.

**Order.** This is built after go-qrdb's verifiable log and go-xkms's transparency log.

## 8. Moves out of this repo (the "deletions")

| Source here | Destination | Kept here |
|---|---|---|
| CA/ACME pieces go-xkms lacks: SPIFFE SAN derivation (`pkg/server/ca_service_adapter.go:240`, duplicated in `pkg/api/rest/xkms_ca_servicer.go:177,191`) | go-xkms `pkg/spiffe` / CA x509-svid profile | caller passes the profile |
| Device-identity issuance wrapper (`pkg/service/certificate_extensions.go:368`, `pkg/server/ca_service_adapter_extensions.go:304`) | go-xkms TCG CA service | transports become the re-export |
| ACME state over qrdb (`pkg/server/acme.go`, `pkg/service/acme_service.go`) | go-xkms ACME, with go-qrdb storage injected through a `With*` option | the injection wiring in `pkg/server` |
| Fleet node cert profile building (`pkg/service/fleet_cert_issuer.go`) | go-xkms workload-identity profile | the enrollment flow (ours) |
| Second datastore: `pkg/catalog` Pebble store at `<DataDir>/catalog` (`pkg/server/lifecycle.go:1297`) | qrdb: a qrdb implementation of `catalog.Storage`, with a one-time import of existing data | the `catalog.Storage` interface, `CatalogManager`, contract tests |
| Second datastore: CA key/cert files at `<DataDir>/keys`, `<DataDir>/certs` (`lifecycle.go:316,321`) | go-xkms keystore on qrdb | none |
| Tier 0 `ImageSignature` (`pkg/orchestration/container_trust.go:71`, SDK alias `sdk/go/orchestration/types.go:176`) | go-xkms image verification (DSSE referrers; gtp `pkg/image` moves to go-xkms as its base) | the digest-pinning check and trust tiers |
| Tier 1 `verifyExpectedPCRs` (`pkg/orchestration/container_attestation.go:599`) and `ContainerSpec.ExpectedPCRs` | our Verifier: `AppraiseQuote` (`pkg/service/attestation_verifier.go:787`) against the reference-values store | quote collection, `bindQuotedPCRs` |

When go-xkms has the moved pieces, these are removed: `CAService`/`CertificateService` (`pkg/service/ca.go`, `certificate_service.go`), the CA adapters, `/api/v1/certificate` and `/api/v1/acme` verbs, and `cmd/quicbaas/acme.go`. They are replaced by the go-xkms re-exports on every transport (#1327). `pkg/server/cluster_ca.go` and `qrdb_node_cluster_acme.go` stay as composition wiring.

## 9. Volumes, the boot node and NBD as services, the deployer

### Volumes (row `orchestration-volumes`, #195; the row is on main)

A `VolumeProvider` over go-blockstore (v0.5.0-alpha: `local`, `drbd`, `ha`) supports:
- create;
- grow-only resize;
- attach to a device path;
- promote and demote.

Services declare volumes in their manifest; the agent attaches them at launch.

**The NBD volume store sits behind this row.** go-blockstore volumes are exported over NBD by the boot node's NBD server. Diskless nodes get their root and data volumes from it.

**Seams:**
- go-blockstore supplies the volumes. It has no NBD code; none is added there.
- gtp's NBD export consumes them through `sdk/go`.

### The boot node and NBD as broker-class services (received from gtp)

The code stays gtp's: provisioning and netboot are gtp's in the plan. It lives in gtp `pkg/bootstrap/container`: `Server`, `NBDConfig` and `nbd.go`, `DHCPConfig`, the media set and the payload.

**What gtp delivers:** a signed `Release` (image and manifest).

**What we host:** that release, as a service in the **broker** privilege class. The class grants host network, the raw DHCP/TFTP sockets, the NBD port, and block-device access to the attached go-blockstore volumes.

**What we add:** the lifecycle verbs, the A/B slots, measurement and the transparency-log record, as for any other service.

### Move-in of gtp's bootstrap deployer

| Source (gtp) | Destination | Kept |
|---|---|---|
| `pkg/bootstrap/deployer` (`Deployer`, `LinuxDeployer`, the SSH path, netboot-media streaming), with `linux_test.go`, `ssh_test.go`, `netboot_media_test.go`, `types_test.go` | `pkg/fleet/deploy`, beside `FleetDeployService` | the `Deployer` interface, SSH install, media streaming, `DeployConfig`/`DeployResult`, the tests |

- Its local `docker run`/`docker load` calls (`localRunner`) are re-pointed to the service plane's `deploy` verb. The boot node becomes a broker-class service rather than a docker container.
- `DefaultContainerRuntime = "docker"` and the docker/podman `ContainerRuntime` field are removed, with no docker default left anywhere.
- gtp calls `pkg/fleet/deploy` through `sdk/go` and removes its copy after the move is wired.

## Seams (rows on both sides)

**go-xkms:**
- receives the moved CA/ACME pieces and gtp `pkg/image`;
- image signing and verification (DSSE);
- the x509-svid workload identity;
- peergate re-export;
- the transparency-log append API.

**go-qrdb:** the verifiable-log type, reached through go-xkms.

**gtp:**
- runtime, registry and container-attestor moves out of gtp;
- the `Release` object for staged upgrades;
- registry names to DNS;
- the boot node and NBD as broker-class services, and the deployer move-in (§9);
- the NBD export over go-blockstore volumes (§9).

**The application-PCR seam (both sides implement it).**
- **We** implement the recording side. Our runtime builds the `LaunchMeasurement` (image digest, config hash, policy hash), appends it to the node measurement log, and calls `HostTPM.ExtendPCR` on the application PCR (default 16) before the container starts. No extend means no start.
- **gtp**, as the Attester, implements `HostTPM` (`Quote`, `ReadPCRs`, `ExtendPCR`) on its nodes and supplies it to the fleet agent at startup.
- **Our Verifier** replays the log against the quoted PCR in `AppraiseQuote`.
- **The contract** is the `HostTPM` interface in `pkg/orchestration`, re-exported through `sdk/go`. Each side tests against it.

## Lane order (after rows)

1. Pin bump to the current go-xkms and go-qrdb, after go-xkms's lanes (DS-1737).
2. Tier 1 move to `AppraiseQuote`, then the catalog onto qrdb (no new dependency).
3. Runtime move, with the co-location fixes in §1 (the cgo agent build, a local install, the work-dir lock).
4. Services, verbs and controller, then the deployer move-in.
5. Registry move.
6. Measured launch with the `HostTPM` seam, and default deny, then policy.
7. Volumes over go-blockstore, then hosting the boot node and NBD as broker-class services.
8. CA/ACME moves and the transparency-log appender, as go-xkms ships.
