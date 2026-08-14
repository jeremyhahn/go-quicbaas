# Container Trust

Container trust answers one question about an orchestrated agent: **is it
running the code we approved, and can the host prove it?**

That is a different question from the one the sandbox answers ("is the agent
isolated from the host?"), and neither answer implies the other. A container can
be perfectly isolated while running an image someone swapped under a mutable
tag, and a digest-pinned image can run with no sandbox at all. QuicBaaS
therefore resolves the two on separate axes. This document covers the trust
axis; see [Sandbox Policy](../agents/daemon.md#sandbox-policy-reference) for the
other one, and the [orthogonal sandbox axis](#the-orthogonal-sandbox-axis)
section below for how they interact.

Source: `pkg/orchestration/trust_policy.go`, `container_trust.go`,
`container_attestation.go`, `container_runtime_digest.go`.

## Trust Levels

`TrustLevel` is the per-request integrity tier. It is the trust-axis
counterpart of `SandboxMode`.

| Level | Wire name | Tier | Requires |
|-------|-----------|------|----------|
| `TrustLevelDefault` | `default` | — | Nothing; defers to the server's `TrustPolicy`. Zero value. |
| `TrustLevelNone` | `none` | — | Nothing. Mutable tags allowed, no attestation. What every execution did before the trust axis existed. |
| `TrustLevelPinned` | `pinned` | 0 | Launch from an immutable content digest (`repository@sha256:<hex>`), verified after launch. No hardware. |
| `TrustLevelAttested` | `attested` | 1 | Tier 0 plus a host TPM quote verified against the spec's golden PCRs. |
| `TrustLevelMeasured` | `measured` | 2 | Tier 1 plus measured launch: image and configuration digest extended into a PCR *before* the container starts. |

Because `TrustLevelDefault` is the Go zero value and `TrustPolicyOptIn` is the
zero-value policy, a deployment that has never been configured for trusted
computing behaves exactly as it did before the trust axis existed.

### Capabilities

`TrustCapabilities` describes what a target host can actually prove. It has one
field per tier:

| Field | Tier | Meaning |
|-------|------|---------|
| `DigestPinning` | 0 | The container runtime can launch and verify an image by content digest. |
| `HostTPM` | 1 | The host exposes a TPM the runtime can quote. |
| `MeasuredLaunch` | 2 | The host permits extending the measurement PCR before launch. |

The zero value advertises no capability at all, which is the correct posture for
a host that has not been probed: a required tier then fails closed rather than
silently downgrading. A remote (daemon-dispatched) spawn advertises no local
capability, so it is refused above `opt-in` until that daemon proves the tier
itself.

## Resolution

`ResolveTrustLevel(requested, serverPolicy, tenantOverride, caps)` produces the
effective tier. It is deny-by-default in **both** directions:

- A tenant override may only **tighten** the server policy. A weaker override is
  ignored, so a tenant with a stricter compliance obligation can raise the floor
  without the operator tightening the whole deployment, and a tenant record can
  never lower it.
- An explicit level **below** the effective floor is rejected with
  `TrustLevelViolationError`. A caller cannot opt out of a tier the operator
  requires.
- A level the host **cannot prove** is rejected rather than downgraded. The one
  exception is a `default` request under `TrustPolicyOptIn`, which resolves to
  `none` because opt-in means "no trust requirement at all".

The server policy is configuration:

```yaml
orchestration:
  trust_policy: pinned   # opt-in (default) | pinned | attested | measured
```

Each policy sets the floor that `default` resolves to and that nothing may fall
below: `opt-in` → `none`, `pinned` → Tier 0, `attested` → Tier 1, `measured` →
Tier 2.

`SpawnSession` resolves the level at the same pre-side-effect chokepoint that
resolves the sandbox mode, and the **resolved** level — never the caller's
request — is what reaches the container spec and the backend.

### Re-resolution on resume

The resolved level is persisted on the session record, and a resume
**re-resolves** it rather than replaying it
(`orchestrator_resume.go`). The recorded level is what the session was
originally admitted at, but a floor the operator has raised since must apply to
the process being started *now*. A session that no longer clears the floor fails
to resume instead of quietly reconstituting at its old tier.

## Tier 0 — Image Identity Pinning

Tier 0 requires no hardware and no host cooperation, so it is the only tier the
default `ExecContainerRuntime` implements. It answers exactly one question: is
the agent running the bytes we approved?

**Pinning.** `ContainerSpec.ImageDigest` pins the container to a canonical OCI
content digest, or the digest may be supplied inline as
`repository@sha256:<hex>` in `Image`. When both are present they must agree —
disagreement is an `ImageDigestMismatchError` at validation time rather than a
silent preference for one of the two. `ValidateImageDigest` accepts only SHA-256
in canonical lowercase hex; OCI permits other algorithms in principle, but
accepting one keeps the comparison surface small and matches every registry in
practice.

**Post-launch verification.** `VerifyPinnedImage` confirms the container the
runtime actually created came from the pinned image. Note that a registry
*manifest* digest and a *local image ID* are different values, so the check
resolves the pinned reference to its local image ID first and compares like with
like — both values coming from the runtime's own inspect output. A substituted
image, which is the exact attack digest pinning exists to stop, produces a
`ContainerImageDigestMismatchError`.

The check is skipped for unpinned specs, and skipped when the container cannot
be inspected yet: an interactive container is created asynchronously by the
foreground run process, and failing a launch because the runtime had not
finished creating the container would be a false positive. The command-line pin
still applies in that window, since the runtime cannot resolve
`repository@digest` to anything other than that digest.

**Detached signatures.** `ImageSignature` optionally carries endorsement
material for the pinned digest: a signature over the ASCII digest string plus
the signer's certificate chain, leaf first. The signed payload is the digest
string rather than a repository name, so an endorsement is portable across
registries and mirrors — it binds the approving key to image *content*.
Verification is delegated to the go-xkms attestation verifier through
`Security().Attestation()`; nothing in `pkg/orchestration` reimplements a
signature check or shells out to an external tool.

This is deliberately **not** registry-native (cosign / sigstore) verification.
That needs a registry client and a Rekor transparency-log client, neither of
which is available in-repo, and adding either belongs in its own change rather
than behind a stub.

### The configuration digest

`ContainerConfigDigest(spec)` is a deterministic SHA-256 over the parts of a
spec that decide what the container executes, what it can see, and what it is
allowed to do: the pinned image reference, entrypoint, command, args,
environment, mounts (including each mount's kind), ports, working directory,
network, user, GPU passthrough, auto-remove, labels, the trust label, **the
privileged flag, and the capability add/drop sets**.

Privilege is measured for the same reason image content is. A container flipped
to `Privileged`, or granted `CAP_SYS_ADMIN`, is a different workload from the
one an operator approved, and a digest that ignored the difference would certify
the safe configuration while the dangerous one ran.

Determinism is the whole point, because this digest is extended into a PCR at
measured launch and two launches of the same approved configuration must produce
the same value:

- Every map is serialized in sorted key order. Go randomizes map iteration order
  per process, so a naive range over `Env` or `Labels` would silently make
  measurements non-reproducible.
- Every field is length-prefixed, so no combination of values can alias another.
- Capability lists are sorted before hashing, and the runtime emits them in that
  same sorted order, so the measured spec and the launched container cannot
  disagree.
- Mounts are hashed in declaration order, because they are order-significant on
  the command line.

Two things are deliberately **excluded**: `Name`, a per-instance identifier that
is frequently random; and the trust-verification metadata itself (`TrustLevel`,
`ExpectedPCRs`, `ImageSignature`, `MeasurementPCR` / `MeasurementBank`), which
describes how a launch is *checked* rather than what is launched. Including the
latter would make the measurement depend on the policy that verifies it.

`TrustLabel` **is** included: it is an operator-assigned part of the approved
identity, so one image and configuration can serve two distinct measured
identities.

## Tiers 1 and 2 — Host Attestation and Measured Launch

Both tiers compose go-xkms `pkg/tpm2`. Nothing in QuicBaaS reimplements a TPM
primitive.

`HostTPM` is the narrow capability the attesting runtime needs — `Quote`,
`ReadPCRs`, `ExtendPCR`. It is satisfied as-is by go-xkms `*tpm2.TPM2` and by
anything implementing xkms's `TrustedPlatformModule`, including its software
simulator, so no adapter type exists between the two projects.

`TPMAttestingRuntime` is the reference `AttestingContainerRuntime`. It wraps a
base runtime (normally `ExecContainerRuntime`, which supplies Tier 0) and adds
attestation on top. Both are required at construction: an attesting runtime with
no TPM would silently degrade to Tier 0 while advertising more.

### Tier 1 — the host quote

`Attest` quotes the union of the spec's expected PCR indices and, for a measured
launch, the measurement PCR. A host whose firmware, bootloader or kernel changed
produces different PCRs and is refused before an agent is scheduled onto it
(`PCRMismatchError`). A missing quoted PCR is a mismatch, not a pass — absence
of evidence is not evidence of a match.

### Tier 2 — measured launch

Immediately **before** the container starts, `LaunchMeasurement` is extended
into the measurement PCR. A measurement taken afterwards would not be a measured
launch; it would be an assertion that something already running was approved.

The measurement is a newline-free, NUL-separated record so a verifier can
reconstruct it from the spec alone:

```
"quicbaas-container-launch/v1" \0 <imageDigest> \0 <configDigest> \0 <trustLabel>
```

PCR 16 (`MeasurementPCRDefault`) is the default target. It is the TCG PC Client
"debug" register: resettable, not consumed by firmware or the bootloader, and
the conventional home for application-supplied measurements, so extending it
cannot disturb a platform's boot-integrity PCRs. `MeasurementBank` defaults to
`sha256`, the mandatory bank on every TPM 2.0 PC Client platform.

The resulting quote proves not merely that the host booted correctly, but that
*this exact configuration* was the thing launched on it — a property no post-hoc
inspection of a running container can establish.

### Binding the PCR read to the signed quote

This is the subtlest part of the design and the reason the golden-value
comparison sits *downstream* of a binding step.

A `TPM2_Quote` signs a `TPMS_ATTEST` structure whose `TPMS_QUOTE_INFO` carries
the PCR selection and a single `pcrDigest` — `H(PCR[i1] || PCR[i2] || …)` over
the selected registers in selection order. It does **not** carry the individual
PCR values, so those must come from a separate, unsigned `TPM2_PCR_Read`.

Comparing golden values against that read alone establishes nothing: the read
carries no signature, and a PCR can be extended in the window between the quote
and the read. `bindQuotedPCRs` closes both gaps before any golden value is
looked at:

- **Freshness.** The nonce must be inside the signed body, not merely alongside
  it. `VerifyQualifyingData` compares `extraData` exactly.
- **Agreement.** `pcrDigest` is recomputed over the values that were read, in
  the order the *signed* selection names them, and must equal the digest inside
  the signed body. A TOCTOU change between quote and read, or a tampered read
  path, makes the recomputation differ and is rejected as
  `QuotedPCRDigestMismatchError`.

Two further failure modes are errors rather than fallbacks. A PCR selected in
more than one bank with differing values would make the index-to-value map
ambiguous, and a golden comparison against an ambiguous value is not a
comparison. A bank that is genuinely absent from the read is
`PCRBankUnavailableError`, never a fallback to whichever bank happened to be
first — reading PCRs from an unintended bank produces measurements that look
valid and mean nothing.

What binding does **not** establish is that the signature over the quote body
verifies under a trusted attestation key. That needs the AK public key and its
certificate chain, neither of which the narrow `HostTPM` seam exposes, and it
belongs to the remote verifier that consumes `ContainerAttestation.Quoted` and
`.Signature`. Locally, the quote body is trusted because it came from the host's
own TPM handle; what is being defended against is a divergence between that body
and the separate PCR read.

### The evidence record

`ContainerAttestation` is self-contained — a remote verifier needs the quote,
the nonce it was bound to, the PCR values, and the identity of what was
launched, and all four are present. Every value in `PCRs` has been bound to
`Quoted`; values that fail that check never reach a caller. `PCRBank` names the
bank the *signed selection* dictated, not one the spec guessed at, so a verifier
does not have to infer it.

## Spec Fields

Every trust field on `ContainerSpec` is inert at its zero value, so a spec
written before the trust dimension existed launches byte-for-byte the same
command line it always did.

| Field | Tier | Purpose |
|-------|------|---------|
| `ImageDigest` | 0 | Canonical `sha256:<hex>` content pin. |
| `ImageSignature` | 0 | Detached endorsement over the digest, plus signer chain. |
| `TrustLevel` | — | Required tier; `default` defers to server policy. |
| `TrustLabel` | 0+ | Operator-assigned workload identity; folded into the config digest. |
| `ExpectedPCRs` | 1 | Golden PCR set, keyed by index. Required at `attested` and above. |
| `MeasurementPCR` | 2 | PCR extended at measured launch. Defaults to 16. |
| `MeasurementBank` | 2 | `sha1` \| `sha256` \| `sha384` \| `sha512`. Defaults to `sha256`. |

Validation is offline and deterministic: a spec requiring Tier 0 or above
without a pin is rejected at `Validate` time (`ImageNotPinnedError`), not at
launch, and `attested` without any expected PCR is rejected the same way.

## Errors

Every failure is a typed error wrapping an `Err*` sentinel, so callers can match
either the specific condition or the class with `errors.Is`.

| Sentinel | Raised when |
|----------|-------------|
| `ErrImageNotPinned` | A tier requires a digest pin but the spec supplies only a mutable tag. |
| `ErrImageDigestMismatch` | A resolved image digest disagrees with the pinned one. |
| `ErrImageDigestUnresolved` | The runtime could not determine an image's content digest. |
| `ErrAttestationUnavailable` | The capability a tier requires is not present on this host. |
| `ErrPCRMismatch` | A quoted PCR does not match its golden value. |
| `ErrMeasurementFailed` | The measured-launch PCR extension failed, so the container was not started. |
| `ErrQuoteFailed` | The host TPM refused or failed to produce a quote. |
| `ErrQuoteNotBound` | PCR values could not be tied to the signed quote body. |
| `ErrQuotedPCRDigestMismatch` | The recomputed `pcrDigest` disagrees with the signed one. Also reports `ErrQuoteNotBound`. |
| `ErrPCRBankUnavailable` | A required PCR bank is not among those the TPM reported. |

## What QuicBaaS Owns, and What It Does Not

`TPMAttestingRuntime` is the **standalone** implementation and it is complete on
its own: QuicBaaS has no dependency on go-trusted-platform, and a single-node
deployment needs nothing further.

Fleet-grade trust is a different problem, and it is go-trusted-platform's: a
device registry that resolves hosts to their approved identities, scheduled
re-attestation, golden-PCR policy, and quarantine for hosts that drift. None of
that belongs in QuicBaaS.

Two seams exist for it to fill:

- **`AttestingContainerRuntime`** (`pkg/orchestration/container_attestation.go`),
  re-exported for SDK-bound consumers as `sdk/go/orchestration`. That package is
  pure type aliases with no behavior, so a value produced by `pkg/orchestration`
  is the same value there and `errors.Is`, `errors.As` and type assertions work
  across the boundary unchanged. Its completeness contract is closure, not
  totality:
  every exported identifier reachable from an alias — through an interface
  method signature, an exported struct field, or a constructor parameter — is
  itself aliased, so a consumer that can name a type can always name every
  argument it must supply.
- **`ContainerAttestor`**, registered through the
  `quicbaas.WithContainerAttestor` server option. It mirrors the `DANEPublisher`
  seam exactly: QuicBaaS declares the interface, ships a nil default, and returns
  `ErrAttestationNoAttestor` until an embedder registers one.
  `NewRuntimeContainerAttestor` fills the seam with the standalone tiers for
  deployments that do not need a fleet. Implementations **must** fail closed: a
  host that cannot prove its state returns an error, never an unattested success.

## The Orthogonal Sandbox Axis

The sandbox axis asks whether the agent is isolated from the host. It resolves
with the same shapes as the trust axis — a per-request mode, a server policy, an
optional tenant override that may only tighten — and it also denies by default,
but it is a separate decision throughout.

| Dimension | Type | File | Question |
|-----------|------|------|----------|
| Containment | `SandboxMode` / `SandboxPolicy` | `sandbox_policy.go` | Is the agent in a container at all? |
| Network | `SandboxNetwork` | `sandbox_network.go` | Can the agent reach the host's loopback services? |
| Filesystem | mount allowlist | `mount_policy.go` | Which host paths may be bind-mounted in? |

Network isolation is worth calling out because it is easy to lose: a container
run with `--network host` joins the host's own network namespace, so every
loopback-bound service reaches it — the daemon's admin listeners, local metrics
endpoints, a `127.0.0.1` datastore — precisely the services whose host firewall
rules assume no container can reach them. The default is therefore an isolated
namespace (`bridge`), and host networking is available only as an explicit
operator decision in configuration, never as something a spawn request can
select.

Bind mounts are deny-by-default: only sources under the requesting tenant's
subtree of the managed projects root, or under an operator-configured allowlist
root, are permitted.

Full operator reference: [Remote Agent Daemon](../agents/daemon.md) —
[Sandbox Policy](../agents/daemon.md#sandbox-policy-reference),
[Sandbox Network](../agents/daemon.md#sandbox-network-reference),
[Bind-Mount Allowlist](../agents/daemon.md#bind-mount-allowlist-reference).

## See Also

- [Agent Orchestration Security](security.md) — the security surface this
  document expands on.
- [Agent Orchestration Overview](overview.md) — the orchestration layer as a
  whole.
- [Remote Agent Daemon](../agents/daemon.md) — operator reference for the
  sandbox axis.
