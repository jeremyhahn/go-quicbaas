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

	"github.com/jeremyhahn/go-quicbaas/pkg/agent/deploy"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Agent Deploy Flags
// =============================================================================
//
// Operator-facing flags for `quicbaas agent deploy`. Each flag maps 1:1
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
	deploySystemdFlag     string
)

// =============================================================================
// Factory Plumbing
// =============================================================================
//
// The factory indirection lets tests substitute a stub service without
// opening a real SSH connection.

// AgentDeployServiceFactory returns a configured AgentDeployService.
// Follows the same pattern as orchestratorClientFactory.
type AgentDeployServiceFactory func() (service.AgentDeployService, error)

// agentDeployServiceFactory is the production wiring: SSH primitives
// from pkg/agent/deploy + an SDK-backed agent lister.
var agentDeployServiceFactory AgentDeployServiceFactory = createAgentDeployService

// createAgentDeployService wires the SDK orchestrator client as the
// agent-lister dependency so the "wait for online" poll hits the real
// server registry. It also constructs an xkms SSH client via the
// environment (XKMSD_URL / XKMSD_TOKEN for the REST transport; embedded
// file-backed store under the user's home directory otherwise) and
// injects it as an SSHSignerResolver so `--ssh-key-name` resolves
// against the operator's xkms-backed key store, and as an
// SSHHostKeyResolver so the target's host key is verified against its
// pin in the same store.
//
// The AgentCertIssuer dependency is resolved from the in-process server
// when the CLI is running alongside quicbaasd (embedded transport);
// external transports return an error that surfaces to the operator so
// they know the CLI needs to be run from the QuicBaaS host.
func createAgentDeployService() (service.AgentDeployService, error) {
	deps := service.AgentDeployServiceDeps{}

	// Orchestrator client is only required when --wait-online > 0, so
	// a failure here is tolerated — the service degrades gracefully.
	if lister, err := orchestratorClientFactory(); err == nil {
		deps.AgentLister = lister
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

	// AgentCertIssuer: required for the deploy to succeed. Resolved
	// from the in-process server CA when running embedded.
	issuer, err := resolveAgentCertIssuer()
	if err != nil {
		return nil, err
	}
	deps.CertIssuer = issuer

	// MCP endpoint: empty unless an embedded server published one. When set,
	// every deployed daemon is started with --mcp-endpoint so its spawned
	// agents can resolve the "quicbaas" MCP preset against the long-lived mTLS
	// identity this deploy uploads.
	deps.MCPEndpoint = resolveDeployMCPEndpoint()

	return service.NewAgentDeployService(deps), nil
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

// newAgentDeployCommand builds the `quicbaas agent deploy` subcommand
// that is attached to the existing `agent` group via its init path.
func newAgentDeployCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy the QuicBaaS agent to a remote host over SSH",
		Long: `Deploy a QuicBaaS agent daemon to a remote host.

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
      quicbaas ssh known-hosts add --host <h> --algorithm <a> --fingerprint <f>
  The pin is read under the same scope/owner as the SSH key, so
  --ssh-key-scope / --ssh-key-owner select both. Pass
  --ssh-insecure-host-key to skip verification on a lab host.

Examples:
  quicbaas agent deploy --ssh alice@node01 --ssh-key-name alice-laptop \
    --server https://quicbaas.example.com:8443 --tenant acme \
    --backend acp --binary bin/quicbaas-static`,
		RunE: runAgentDeploy,
	}

	cmd.Flags().StringVar(&deploySSH, "ssh", "", "SSH target in user@host[:port] form (required)")
	cmd.Flags().StringVar(&deploySSHKeyName, "ssh-key-name", "", "Name of an xkms-stored SSH key (private material stays in the server). Stage keys with `quicbaas ssh keys import` first.")
	cmd.Flags().StringVar(&deploySSHKeyScope, "ssh-key-scope", "", "Scope for --ssh-key-name lookup: user | tenant | service. Defaults to 'user'.")
	cmd.Flags().StringVar(&deploySSHKeyOwner, "ssh-key-owner", "", "Owner identifier for --ssh-key-name lookup (defaults to current user for scope=user, current tenant for scope=tenant, 'default' for scope=service).")
	cmd.Flags().StringVar(&deploySSHPassword, "ssh-password", "", "SSH password fallback (prefer --ssh-key-name when possible)")
	cmd.Flags().BoolVar(&deployInsecureHostKey, "ssh-insecure-host-key", false, "Skip remote host key verification (operator opt-in). Without it the target's key must be pinned in the SSH known-hosts store via `quicbaas ssh known-hosts add`, under the same scope/owner as --ssh-key-scope / --ssh-key-owner.")
	cmd.Flags().StringVar(&deployServer, "server", "", "QuicBaaS server address the agent will dial (required)")
	cmd.Flags().StringVar(&deployBindAddr, "bind", "0.0.0.0:9091", "Local bind address the agent exposes on the remote host")
	cmd.Flags().StringSliceVar(&deployBackends, "backend", []string{"acp"}, "Agent backend to enable (repeat for multiple)")
	cmd.Flags().StringVar(&deployBinary, "binary", "bin/quicbaas-static", "Local path to the static CLI binary")
	cmd.Flags().StringVar(&deployInstallDir, "install-dir", "", "Remote install directory (default ~/.quicbaas/bin)")
	cmd.Flags().StringVar(&deployDataDir, "data-dir", "", "Remote agent work directory (default ~/.quicbaas/agent)")
	cmd.Flags().DurationVar(&deployWaitOnline, "wait-online", 2*time.Minute, "How long to wait for the agent to report online; 0 to skip")
	cmd.Flags().StringVar(&deploySystemdFlag, "systemd", "auto", "Daemon supervisor: auto | yes | no")

	for _, flag := range []string{"ssh", "server"} {
		if err := cmd.MarkFlagRequired(flag); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to mark %s required: %v\n", flag, err)
		}
	}

	// Reject conflicting SSH auth flags at parse time. The service
	// layer also checks this invariant; rejecting here gives operators
	// a fast, clear error message.
	cmd.MarkFlagsMutuallyExclusive("ssh-key-name", "ssh-password")

	return cmd
}

// runAgentDeploy orchestrates flag parsing → service invocation →
// output formatting.
func runAgentDeploy(cmd *cobra.Command, args []string) error {
	spec, err := buildDeploySpec()
	if err != nil {
		return formatError(err)
	}

	svc, err := agentDeployServiceFactory()
	if err != nil {
		return formatError(err)
	}

	// The deploy service talks to the remote host over SSH and may
	// also poll the server registry. Use the global timeout so long
	// deploys share the same budget as other CLI commands.
	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout+spec.WaitOnline)
	defer cancel()

	result, err := svc.Deploy(ctx, spec)
	if err != nil {
		return formatError(err)
	}

	return formatAgentDeployResult(result, agentOutputFmt, os.Stdout)
}

// buildDeploySpec translates the flag surface into a deploy.Spec.
func buildDeploySpec() (*deploy.Spec, error) {
	user, host, err := parseSSHTarget(deploySSH)
	if err != nil {
		return nil, err
	}

	sysFlag, err := parseSystemdFlag(deploySystemdFlag)
	if err != nil {
		return nil, err
	}

	return &deploy.Spec{
		Host:            host,
		SSHUser:         user,
		SSHKeyName:      deploySSHKeyName,
		SSHKeyScope:     deploySSHKeyScope,
		SSHKeyOwner:     deploySSHKeyOwner,
		SSHPassword:     deploySSHPassword,
		InsecureHostKey: deployInsecureHostKey,
		Server:          deployServer,
		Tenant:          globalTenantID,
		AgentBind:       deployBindAddr,
		Backends:        deployBackends,
		Binary:          deployBinary,
		InstallDir:      deployInstallDir,
		DataDir:         deployDataDir,
		Systemd:         sysFlag,
		WaitOnline:      deployWaitOnline,
	}, nil
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

// formatAgentDeployResult pretty-prints a *deploy.Result in the chosen
// output format.
func formatAgentDeployResult(result *deploy.Result, format string, w io.Writer) error {
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
		fmt.Fprintf(tw, "Service Status:\t%s\n", result.ServiceStatus)
		if result.AgentCertPath != "" {
			fmt.Fprintf(tw, "Agent Cert:\t%s\n", result.AgentCertPath)
		}
		if result.AgentCertSerial != "" {
			fmt.Fprintf(tw, "Agent Cert Serial:\t%s\n", result.AgentCertSerial)
		}
		if result.AgentID != "" {
			fmt.Fprintf(tw, "Agent ID:\t%s\n", result.AgentID)
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

// resolveAgentCertIssuer produces the AgentCertIssuer used by the deploy
// service. The issuer must wrap the server's TrustedCA to issue mTLS
// client certificates that the server's register handler will accept.
//
// The concrete wiring lives in bindings_agent_deploy.go (desktop build)
// and bindings_embedded.go (embedded server). The function here is a
// seam so tests can replace the issuer without touching either.
var resolveAgentCertIssuer = defaultResolveAgentCertIssuer

// resolveDeployMCPEndpoint returns the control plane's configured MCP listener
// address (https://host:port/mcp) so the deploy service can start each remote
// daemon with --mcp-endpoint. The default returns an empty string: the
// CLI-only build has no in-process server to read MCP config from, so the
// "quicbaas" preset is left unresolved (current behavior). The desktop build
// overrides this seam to read the endpoint published by the embedded server.
var resolveDeployMCPEndpoint = func() string { return "" }

// defaultResolveAgentCertIssuer falls back to the SDK-backed issuer,
// which calls the running daemon's /api/v1/xkms/ca/* surface over
// whichever transport the CLI is configured to use. This is the
// standard path for `quicbaas agent deploy` invoked from an admin
// workstation against a remote quicbaasd.
//
// Build-tagged embedded seams (e.g. the Wails desktop binding) may
// replace this resolver with a direct in-process CA hook for lower
// latency, but the SDK-backed path always works.
func defaultResolveAgentCertIssuer() (service.AgentCertIssuer, error) {
	return newSDKAgentCertIssuer()
}

// deployNoCertIssuerError surfaces when the CLI is invoked against a
// transport that does not expose the server's CA. Running the deploy
// subcommand requires the embedded server transport so the CA is in
// the same process.
type deployNoCertIssuerError struct{}

// Error implements the error interface.
func (e *deployNoCertIssuerError) Error() string {
	return "agent deploy: no certificate issuer available — run `quicbaas agent deploy` on the QuicBaaS server host so the embedded CA can mint the agent identity"
}
