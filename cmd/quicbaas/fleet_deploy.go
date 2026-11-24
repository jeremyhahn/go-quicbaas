// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/fleet/deploy"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// Fleet Deploy Flags
// =============================================================================
//
// Operator-facing flags for `quicbaas fleet deploy`. Each flag maps 1:1
// onto a field of deploy.Spec. Default values are conservative:
//
//   --install-dir / --data-dir  remain empty so the service applies its
//                               canonical defaults (~/.quicbaas/*).
//   --systemd                   tri-state: unset → auto-detect; true →
//                               force unit install; false → force nohup.
//   --wait-online               defaults to 2 minutes so a slow first
//                               heartbeat doesn't fail the deploy.

var (
	deploySSH             string
	deploySSHKeyName      string
	deploySSHKeyScope     string
	deploySSHKeyOwner     string
	deploySSHPassword     string
	deployInsecureHostKey bool
	deployServer          string
	deployBindAddr        string
	deployBackends        []string
	deployBinary          string
	deployInstallDir      string
	deployDataDir         string
	deployWaitOnline      time.Duration
	deployMaxJobs         int
	deploySystemdFlag     string
	deploySpecFile        string
	deployProjectsRoot    string
	deployPreflightOnly   bool
	deploySkipPreflight   bool
	deployLocal           bool
)

// =============================================================================
// Factory Plumbing
// =============================================================================
//
// The factory indirection lets tests substitute a stub service without
// opening a real SSH connection.

// FleetDeployServiceFactory returns a configured FleetDeployService.
// Follows the same pattern as orchestratorClientFactory.
type FleetDeployServiceFactory func() (service.FleetDeployService, error)

// fleetDeployServiceFactory is the production wiring: SSH primitives
// from pkg/fleet/deploy + an SDK-backed fleet node lister.
var fleetDeployServiceFactory FleetDeployServiceFactory = createFleetDeployService

// createFleetDeployService wires the SDK orchestrator client as the
// agent-lister dependency so the "wait for online" poll hits the real
// server registry. It also constructs an xkms SSH client via the
// environment (XKMSD_URL / XKMSD_TOKEN for the REST transport; embedded
// file-backed store under the user's home directory otherwise) and
// injects it as an SSHSignerResolver so `--ssh-key-name` resolves
// against the operator's xkms-backed key store, and as an
// SSHHostKeyResolver so the target's host key is verified against its
// pin in the same store.
//
// The FleetNodeCertIssuer dependency is resolved from the in-process server
// when the CLI is running alongside quicbaasd (embedded transport);
// external transports return an error that surfaces to the operator so
// they know the CLI needs to be run from the QuicBaaS host.
func createFleetDeployService() (service.FleetDeployService, error) {
	deps := service.FleetDeployServiceDeps{}

	// Orchestrator client is only required when --wait-online > 0, and
	// for the fleet-registry removal `fleet stop` / `fleet undeploy` make,
	// so a failure here is tolerated: deploy degrades gracefully, and stop /
	// undeploy refuse with a typed error rather than leave a node the
	// registry still calls running.
	if client, err := orchestratorClientFactory(); err == nil {
		deps.FleetNodeLister = client
		deps.FleetNodeRemover = client
	}

	// SSH resolvers: the signer behind --ssh-key-name and the host-key
	// pin behind the known-hosts store. Both come from one SDK client so
	// the deploy opens a single transport. A failure here is tolerated:
	// a password deploy with --ssh-insecure-host-key needs neither, and
	// a deploy that does need them fails later with a typed error that
	// names the missing piece.
	if signer, hostKeys, err := buildDeploySSHResolvers(); err == nil {
		deps.SSHKeys = signer
		deps.SSHHostKeys = hostKeys
	}

	// FleetNodeCertIssuer: required for the deploy to succeed. Resolved
	// from the in-process server CA when running embedded.
	issuer, err := resolveFleetNodeCertIssuer()
	if err != nil {
		return nil, err
	}
	deps.CertIssuer = issuer

	// MCP endpoint: empty unless an embedded server published one. When set,
	// every deployed daemon is started with --mcp-endpoint so its spawned
	// fleet nodes can resolve the "quicbaas" MCP preset against the long-lived mTLS
	// identity this deploy uploads.
	deps.MCPEndpoint = resolveDeployMCPEndpoint()

	return service.NewFleetDeployService(deps), nil
}

// buildDeploySSHResolvers constructs the two SSH resolvers the deploy
// service needs, both backed by a single SDK SSH client:
//
//   - the signer behind --ssh-key-name, so the private key never leaves
//     the key store, and
//   - the host-key resolver, which reads the target's pin from the same
//     known-hosts store `quicbaas ssh known-hosts add/list` manage.
//
// Both are addressed by the same (scope, owner) pair, which is why
// --ssh-key-scope / --ssh-key-owner select the pin as well as the key:
// one SSH surface, one addressing scheme. Per-ref values from the spec
// override these defaults.
func buildDeploySSHResolvers() (service.SSHSignerResolver, service.SSHHostKeyResolver, error) {
	client, err := buildCLISSHClient()
	if err != nil {
		return nil, nil, err
	}
	owner := cliSSHOwner()
	signer := &service.XKMSSSHSignerResolver{
		Client: client,
		Scope:  "user",
		Owner:  owner,
	}
	hostKeys := &service.XKMSSSHHostKeyResolver{
		KnownHosts: client.KnownHosts(),
		Scope:      "user",
		Owner:      owner,
	}
	return signer, hostKeys, nil
}

// =============================================================================
// Cobra Subcommand
// =============================================================================

// newFleetDeployCommand builds the `quicbaas fleet deploy` subcommand
// that is attached to the existing `agent` group via its init path.
func newFleetDeployCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy the QuicBaaS agent to a remote host over SSH, or to this host",
		Long: `Deploy a QuicBaaS fleet node daemon to a remote host, or with --local
to the host this command runs on.

The command issues an mTLS client certificate for the daemon via the
QuicBaaS server's CA, uploads the static CLI binary and identity
material (agent.crt, agent.key, server-ca.crt) over SFTP, installs
either a systemd --user unit or a nohup start script, and optionally
waits for the agent to report online in the fleet registry.

Prerequisites:
  * SSH access to the target host (key or password auth).
  * A static CLI binary produced by: make build-static-cli
  * This CLI must run on the QuicBaaS server host (embedded transport)
    so the deploy can reach the server's CA to mint the agent cert.
  * For a systemd --user install: lingering enabled for the login user,
    or passwordless sudo so the deploy can enable it. Without lingering
    the user's systemd manager — and the agent with it — is stopped when
    the deploy's SSH session closes. The deploy enables it with
        sudo -n loginctl enable-linger <user>
    and fails, naming that command, when it cannot.

Authentication:
  Stored keys only. Operators stage an SSH key via
      quicbaas ssh keys import <file> --name my-key
  and reference it by name here with --ssh-key-name. Legacy on-disk PEM
  paths are no longer accepted — the private material must live behind
  the xkms-backed SSH key store so it never leaves the server.

Host key:
  The target's SSH host key is verified against its pin in the SSH
  known-hosts store before the dial. Pin it once with
      quicbaas ssh known-hosts add --host <h> --key "$(ssh-keyscan -t ed25519 <h> | cut -d' ' -f2-)"
  The pin is read under the same scope/owner as the SSH key, so
  --ssh-key-scope / --ssh-key-owner select both. Pass
  --ssh-insecure-host-key to skip verification on a lab host.

Node spec:
  --spec names a YAML node spec (deployments/fleet/example.yaml is the
  annotated template): the SSH target, the control plane, and an agent:
  section that is exactly the daemon's config-file schema. The deploy
  renders that section to <install-dir>/agent.yaml and the unit runs
  "quicbaas fleet serve --config" on it. A flag set on the command line
  overrides the spec's value. The spec must declare the node's capacity
  (agent.max_concurrent_jobs, or --max-concurrent-jobs).

Preflight:
  Every deploy first checks the node without changing it: ssh and the
  pinned host key, the deploy key, systemd --user and lingering, the
  container runtime and docker group, free disk, the worker images and
  their AGENT_UID, and that the node reaches the control plane over TLS
  verified by the deploy's CA. Each failure names its fix. --preflight
  runs only the checks; --skip-preflight deploys without them.

Local install:
  --local installs on this host with no ssh and no sshd: commands run
  as the current user and files are written in place. The preflight,
  the rendered unit or start script, and --systemd are the deploy's
  own; only the ssh, host-key and deploy-key checks are skipped. --local
  cannot be combined with --ssh or with a spec that names a host.

Bind address:
  --bind defaults to the node's advertised host on port 9091, never to a
  wildcard: the ssh target's host, or for --local the host of this
  node's raft.raft_address. A wildcard bind exposes the agent on every
  interface; pass --bind 0.0.0.0:9091 to ask for that explicitly.

Examples:
  quicbaas fleet deploy --spec deployments/fleet/agent-01.yaml --preflight
  quicbaas fleet deploy --spec deployments/fleet/agent-01.yaml

  quicbaas fleet deploy --ssh alice@node01 --ssh-key-name alice-laptop \
    --server https://quicbaas.example.com:8443 --tenant acme \
    --backend acp --binary bin/quicbaas-agent --max-concurrent-jobs 4`,
		PreRunE: relaxRequiredFlagsForSpecFile("ssh", "server"),
		RunE:    runFleetDeploy,
	}

	registerDeployTargetFlags(cmd)
	cmd.Flags().StringVar(&deployServer, "server", "", "QuicBaaS server address the agent will dial (required)")
	cmd.Flags().StringVar(&deployBindAddr, "bind", "", "Address the agent listens on (default: the node's advertised host, port "+deploy.DefaultNodePort+")")
	cmd.Flags().StringSliceVar(&deployBackends, "backend", []string{"acp"}, "Backend the node serves, by name (claude, codex, opencode, openvino, ollama-agent, vllm, ollama) or the \"acp\" group; repeat for multiple")
	cmd.Flags().StringVar(&deployBinary, "binary", "bin/quicbaas-agent", "Local path to the linux agent binary (make build-agent; cgo, statically linked)")
	registerWaitOnlineFlag(cmd)
	cmd.Flags().StringVar(&deploySystemdFlag, "systemd", "auto", "Daemon supervisor: auto | yes | no")
	cmd.Flags().IntVar(&deployMaxJobs, "max-concurrent-jobs", 0, "Jobs this node can run at once (the spec's agent.max_concurrent_jobs). Required unless the spec declares it: a node with no declared capacity leaves the fleet's capacity undeclared.")
	cmd.Flags().BoolVar(&deployPreflightOnly, "preflight", false, "Run the preflight checks and report; change nothing on the node")
	cmd.Flags().BoolVar(&deploySkipPreflight, "skip-preflight", false, "Deploy without running the preflight checks first")
	if err := cmd.MarkFlagRequired("server"); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to mark server required: %v\n", err)
	}
	cmd.MarkFlagsMutuallyExclusive("preflight", "skip-preflight")
	cmd.Long += deployLocalExample

	return cmd
}

// deployLocalExample closes the deploy help's examples with a local install.
const deployLocalExample = `

  quicbaas fleet deploy --local --server https://quicbaas.example.com:8443 \
    --tenant acme --max-concurrent-jobs 4`

// runFleetDeploy orchestrates flag parsing → service invocation →
// output formatting.
// deployContext binds the operator's deploy tenant to parent: the spec's
// tenant when it names one, otherwise the system tenant. A spec that names
// no tenant is given the system tenant too, so the service's validation and
// the node certificate it mints see the same tenant the context carries.
// The deploy service runs in this process and its tenantctx.Target check
// refuses a context with no bound caller; the remote calls it makes (cert
// minting through the server) are authorised by the server over mTLS
// (go-quicbaas#2229).
func deployContext(parent context.Context, spec *deploy.Spec) context.Context {
	if spec.Tenant == "" {
		spec.Tenant = tenantctx.SystemTenantID
	}
	return tenantctx.WithTenant(parent, spec.Tenant)
}

func runFleetDeploy(cmd *cobra.Command, args []string) error {
	spec, err := resolveDeploySpec(commandFlags(cmd))
	if err != nil {
		return formatError(err)
	}
	spec.SkipPreflight = deploySkipPreflight

	svc, err := fleetDeployServiceFactory()
	if err != nil {
		return formatError(err)
	}

	// The deploy service talks to the remote host over SSH and may
	// also poll the server registry. Use the global timeout so long
	// deploys share the same budget as other CLI commands.
	ctx, cancel := context.WithTimeout(deployContext(context.Background(), spec), globalTimeout+spec.WaitOnline)
	defer cancel()

	if deployPreflightOnly {
		report, err := svc.Preflight(ctx, spec)
		if report != nil {
			if fmtErr := formatPreflightReport(report, fleetOutputFmt, os.Stdout); fmtErr != nil {
				return fmtErr
			}
		}
		if err != nil {
			return formatError(err)
		}
		return nil
	}

	result, err := svc.Deploy(ctx, spec)
	if err != nil {
		return formatError(err)
	}

	return formatFleetDeployResult(result, fleetOutputFmt, os.Stdout)
}

// buildDeploySpec translates the flag surface into a deploy.Spec, for a
// deploy with no --spec file.
func buildDeploySpec() (*deploy.Spec, error) {
	var user, host string
	if !deployLocal {
		var err error
		if user, host, err = parseSSHTarget(deploySSH); err != nil {
			return nil, err
		}
	}

	sysFlag, err := parseSystemdFlag(deploySystemdFlag)
	if err != nil {
		return nil, err
	}
	if deployMaxJobs < 0 {
		return nil, &deploy.InvalidSpecError{Field: "max_concurrent_jobs", Reason: "must be zero (undeclared) or a positive job count"}
	}

	spec := deploy.DefaultSpec()
	spec.Host = host
	spec.SSHUser = user
	spec.SSHKeyName = deploySSHKeyName
	spec.SSHKeyScope = deploySSHKeyScope
	spec.SSHKeyOwner = deploySSHKeyOwner
	spec.SSHPassword = deploySSHPassword
	spec.InsecureHostKey = deployInsecureHostKey
	spec.Server = deployServer
	spec.Tenant = globalTenantID
	spec.NodeBind = deployBindAddr
	spec.Backends = deployBackends
	spec.Binary = deployBinary
	spec.InstallDir = deployInstallDir
	spec.DataDir = deployDataDir
	spec.ProjectsRoot = deployProjectsRoot
	spec.Systemd = sysFlag
	spec.WaitOnline = deployWaitOnline
	spec.Node.MaxConcurrentJobs = deployMaxJobs
	return spec, nil
}

// parseSSHTarget splits a "user@host[:port]" string. Returning a typed
// error keeps downstream plumbing consistent with the rest of the CLI.
func parseSSHTarget(target string) (user, host string, err error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", "", &deploy.InvalidSpecError{Field: "ssh", Reason: "ssh target is required"}
	}
	at := strings.IndexByte(target, '@')
	if at <= 0 || at == len(target)-1 {
		return "", "", &deploy.InvalidSpecError{Field: "ssh", Reason: "expected user@host form"}
	}
	return target[:at], target[at+1:], nil
}

// parseSystemdFlag converts the "auto|yes|no" CLI value into the
// tri-state *bool accepted by deploy.Spec.
func parseSystemdFlag(value string) (*bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return nil, nil
	case "yes", "true", "on":
		v := true
		return &v, nil
	case "no", "false", "off":
		v := false
		return &v, nil
	default:
		return nil, &deploy.InvalidSpecError{Field: "systemd", Reason: "expected auto|yes|no"}
	}
}

// formatFleetDeployResult pretty-prints a *deploy.Result in the chosen
// output format.
func formatFleetDeployResult(result *deploy.Result, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(result)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Host:\t%s\n", result.Host)
		fmt.Fprintf(tw, "Installed Path:\t%s\n", result.InstalledPath)
		if result.ConfigPath != "" {
			fmt.Fprintf(tw, "Config:\t%s\n", result.ConfigPath)
		}
		fmt.Fprintf(tw, "Service Status:\t%s\n", result.ServiceStatus)
		if result.NodeCertPath != "" {
			fmt.Fprintf(tw, "Agent Cert:\t%s\n", result.NodeCertPath)
		}
		if result.NodeCertSerial != "" {
			fmt.Fprintf(tw, "Agent Cert Serial:\t%s\n", result.NodeCertSerial)
		}
		if result.NodeID != "" {
			fmt.Fprintf(tw, "Agent ID:\t%s\n", result.NodeID)
		}
		if result.OnlineAt != "" {
			fmt.Fprintf(tw, "Online At:\t%s\n", result.OnlineAt)
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "deployed %s -> %s (%s)\n", result.Host, result.InstalledPath, result.ServiceStatus)
		return nil
	}
}

// resolveFleetNodeCertIssuer produces the FleetNodeCertIssuer used by the deploy
// service. The issuer must wrap the server's TrustedCA to issue mTLS
// client certificates that the server's register handler will accept.
//
// The concrete wiring lives in bindings_fleet_deploy.go (desktop build)
// and bindings_embedded.go (embedded server). The function here is a
// seam so tests can replace the issuer without touching either.
var resolveFleetNodeCertIssuer = defaultResolveFleetNodeCertIssuer

// resolveDeployMCPEndpoint returns the control plane's configured MCP listener
// address (https://host:port/mcp) so the deploy service can start each remote
// daemon with --mcp-endpoint. The default returns an empty string: the
// CLI-only build has no in-process server to read MCP config from, so the
// "quicbaas" preset is left unresolved (current behavior). The desktop build
// overrides this seam to read the endpoint published by the embedded server.
var resolveDeployMCPEndpoint = func() string { return "" }

// defaultResolveFleetNodeCertIssuer falls back to the SDK-backed issuer,
// which calls the running daemon's /api/v1/xkms/ca/* surface over
// whichever transport the CLI is configured to use. This is the
// standard path for `quicbaas fleet deploy` invoked from an admin
// workstation against a remote quicbaasd.
//
// Build-tagged embedded seams (e.g. the Wails desktop binding) may
// replace this resolver with a direct in-process CA hook for lower
// latency, but the SDK-backed path always works.
func defaultResolveFleetNodeCertIssuer() (service.FleetNodeCertIssuer, error) {
	return newSDKFleetNodeCertIssuer()
}

// deployNoCertIssuerError surfaces when the CLI is invoked against a
// transport that does not expose the server's CA. Running the deploy
// subcommand requires the embedded server transport so the CA is in
// the same process.
type deployNoCertIssuerError struct{}

// Error implements the error interface.
func (e *deployNoCertIssuerError) Error() string {
	return "agent deploy: no certificate issuer available — run `quicbaas fleet deploy` on the QuicBaaS server host so the embedded CA can mint the agent identity"
}
