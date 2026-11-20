# go-quicbaas SPEC rows

The row table: one SPEC row in `docs/SPEC.md` = one row issue = one branch `row/<issue>-<row>` = one lane session = one PR. The row PR closes its row issue and marks the row shipped in `docs/SPEC.md` (handbook `software/conventions/sdlc.md` §2b). Every open issue sits under a row issue, or in one of the kept sets below.

| Row | Row issue | SPEC status | Folded | Open PRs |
|---|---|---|---|---|
| `accounts` | #1678 | in progress | 2 | #2291 |
| `acme` | #1662 | in progress | 1 | none |
| `agent-fleet` | #1680 | in progress | 12 | #2264, #2321, #2385, #2389, #2390 |
| `attestation` | #1663 | in progress | 2 | none |
| `audit-logging` | #1659 | in progress | 4 | #2294, #2410 |
| `authorization` | #1658 | in progress | 4 | #2276, #2382, #2383, #2396 |
| `backup-restore` | #1668 | in progress | 1 | none |
| `barrier` | #1650 | in progress | 5 | none |
| `bootstrap` | #1651 | in progress | 5 | #2401 |
| `certificate-authority` | #1661 | in progress | 7 | #2405 |
| `cluster` | #1649 | in progress | 1 | none |
| `codec` | #1647 | in progress | 0 | none |
| `composition` | #1648 | in progress | 1 | #2249, #2388 |
| `container-runtime` | #1682 | planned | 2 | none |
| `daemon-rest-plane-authentication-and-authorisation` | #2428 | in progress | 1 | none |
| `data-encryption-keys` | #1653 | in progress | 1 | #2377, #2378 |
| `data-services` | #1652 | in progress | 3 | #2384 |
| `declarative-placement` | #1681 | planned | 2 | none |
| `deployment` | #1696 | in progress | 1 | #2265, #2388 |
| `desktop-gui` | #1693 | in progress | 11 | #2376, #2393, #2395, #2415 |
| `devices` | #1667 | planned | 0 | none |
| `embeddings` | #1671 | in progress | 0 | none |
| `go-sdk` | #1689 | in progress | 31 | #2271, #2293, #2384, #2389, #2406, #2411, #2412, #2413, #2414 |
| `hardware-backend-composition` | #1664 | planned | 0 | none |
| `iam` | #1657 | in progress | 35 | #2256, #2285, #2383, #2399, #2402 |
| `identity-refactor` | #1666 | future | 0 | none |
| `kms` | #1654 | in progress | 16 | #2240, #2268, #2279, #2381, #2384, #2409 |
| `mcp-server` | #1692 | in progress | 8 | #2248, #2383, #2388 |
| `measured-launch` | #1685 | planned | 2 | none |
| `multi-tenancy` | #1656 | in progress | 5 | #2416 |
| `multiprotocol-api` | #1690 | in progress | 9 | #2389 |
| `oauth2-oidc` | #1665 | in progress | 9 | #2257, #2383, #2389 |
| `object-storage` | #1673 | in progress | 6 | #2266, #2384 |
| `oci-registry` | #1684 | planned | 1 | none |
| `orchestration` | #1677 | in progress | 12 | #2389 |
| `orchestration-volumes` | #1683 | planned | 1 | none |
| `projects` | #1679 | in progress | 1 | none |
| `rag` | #1672 | in progress | 3 | none |
| `release-readiness` | #1697 | in progress | 16 | #2344, #2349, #2388 |
| `release-signing-key` | #2429 | planned | 1 | none |
| `scheduler` | #1676 | in progress | 5 | #2389 |
| `sdlc` | #1688 | in progress | 17 | #2281, #2282, #2385, #2386, #2387, #2397, #2404 |
| `secrets` | #1655 | in progress | 2 | none |
| `security-hardening` | #1660 | in progress | 2 | #2385, #2398 |
| `service-policy` | #1686 | planned | 1 | none |
| `services` | #1687 | planned | 2 | none |
| `signal-bridge` | #2430 | shipped | 3 | none |
| `software-factory` | #1694 | in progress | 16 | none |
| `ssh` | #1674 | in progress | 2 | none |
| `tenant-agent` | #1695 | future | 0 | none |
| `timeseries` | #1669 | in progress | 0 | none |
| `transparency-log` | #1675 | planned | 1 | none |
| `vector` | #1670 | in progress | 3 | #2389 |
| `websocket` | #1691 | future | 0 | none |

## Main-fix row (kept open)

Red main and the Integration repair (#1704); the main-fix PRs #2343 and #2419 to #2426 carry it.

- #1700 bug: the node's TCP gRPC port does not serve IAM, KMS, DEK, Vector, Session, Audit, PEP, registry or attestati
- #1701 bug: concurrent first-boot on a 3-node cluster races on the bootstrap admin record; the losing node refuses to
- #1702 bug: PIV logins return 401 after cluster bring-up (recurs after #1592)
- #1704 Row: integration suite green under make integration-test, Integration gating again (m787)
- #1731 bug: main red, system-admin PIV login returns 401 in the Integration job (run 59209)
- #1737 bug: main red, gRPC service quicbaas.kms.v1.KeyManagement not registered (kms CLI, scheduler)
- #1785 test(integration): fleet enrolment harness imports go-qrdb pkg/* directly instead of sdk/go re-exports
- #1800 bug: main Integration red, tenant IAM 403 / no tenant bound on data-plane requests (run 61844)
- #1801 bug: main Integration red, QUIC memory backend never ready, project-access tenant missing, worker-image probe 
- #1817 apimodule: register KeyManagement, DEKManagement and Crypto on the node gRPC as an operational stopgap (K1)
- #1962 main red: Devcontainer job, echo v4.15.3 trivy CRITICAL and six requirements behind deps-check
- #1963 main red: Integration, admin PIV login returns 401 when the suites provision a data-plane tenant
- #1964 main red: Integration, quicbaas.kms.v1.KeyManagement is not served on the node gRPC port
- #1966 CI Integration passes when the test cluster never becomes ready (empty exit code)
- #1998 Integration suite: data-plane tenant routes refuse the test clients with "no tenant is bound to the request" (
- #1999 trivy image flags libbrotli1 CVE-2026-111497 HIGH in the integration image
- #2117 main red: the REST listener corrupts TLS records (bad record MAC) and stalls provision past 30 s across six in
- #2118 main red: data put over MCP fails with mcp.callTool: key is required (TestBackendMatrix/file_mcp)
- #2119 main red: gRPC ListJobs maps an unspecified owner type to system and lists no tenant jobs (TestSchedulerMultiP
- #2134 TestAgentAccountUsage/quic fails with 500 on tenant delete
- #2135 TestAdmin_StartImpersonation_Success: StartedAt is empty
- #2136 TestBackendMatrix/file_grpc fails in the integration job
- #2137 TestGracePeriodWithEncryption fails in scheduler integration
- #2179 test(quicbaasd): awaitJob in the SDLC e2e test matches any runner's job, so the resume test returns before the
- #2180 test(fleet_enrollment): adminEdge asserts the agent_accounts grant before anything registers the quicbaas perm
- #2210 ci: CI Gate must need every non-skipped job (Integration) - hollow gate merged #2184
- #2213 fix(deps): spf13/pflag v1.1.0 is past the 7-day line, so deps-check turns every Devcontainer job red
- #2215 fix(security): govulncheck GO-2026-6911 in mcp-go (all versions, no fix) turns make vuln red on main
- #2216 test(integration): data-plane setup refused 403 'no tenant is bound to the request' on main, 25 tests
- #2340 Move the Go toolchain to 1.27.4 (GO-2026-6859)

## Pin-bump chain (kept open)

The go-qrdb and go-xkms integration epic #2370 and its pin issues.

- #1188 Adopt the go-xkms release with no firmware dependency; drop the arduino-generic indirect require
- #1364 go-sdk: bump the go-xkms pin for the attested-caller gate, image signing and workload identity
- #1393 composition: pin go-xkms v0.55.0-alpha (carries the #1531 FIPS fix) and go-qrdb v0.47.0-alpha, then tag v0.16.
- #1720 deps: go-quicraft V1-B API freeze is BREAKING (removals and renames) at the next go-quicraft MINOR
- #1815 release: tag a go-quicbaas release carrying 68410b64e (#1560, embedded Secrets versions routing) for consumers
- #1820 embedded: re-pin go-qrdb for #1084 and #1089; bind the delegated identity on every embedded call; tenant token
- #1919 seam: re-pin to go-qrdb with the sdk/go root re-exports moved into subpackages (go-qrdb #627 slice B, ruling 1
- #2370 epic: integrate the latest go-qrdb and go-xkms, embedded and remote, and remove the old-pin shims

## Owner decisions (kept open, linked from their row)

Each waits on the owner (scope, cost or architecture).

- #213 sdlc: publishing subprocess output is best-effort by construction — decide whether a structured public summary
- #448 test: 728 time.Sleep calls in tests, ~133 of them waiting on background work — decide whether to sweep deliber
- #722 orchestration: the task spawn claim (#670) is per-process — two NODES can still both claim one pending task; m
- #2417 Security: external-issue approval gate with a per-project approver mode (human | triage agent | both)

## Release gate tracker (kept open)

Linked from the `release-readiness` row.

- #1133 Release readiness: public release
