// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/fleet/deploy"
)

// The node-spec layer of `fleet deploy` and of the lifecycle commands
// (`fleet undeploy|stop|start`). They share one flag set for naming a node —
// --spec, --ssh and the SSH credential flags, the install and data
// directories — bound to the same package variables, so a node is addressed
// identically by every command that touches it.

// registerDeployTargetFlags adds the flags that name a node and how to reach
// it.
func registerDeployTargetFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&deploySpecFile, "spec", "", "Node spec file (YAML; see deployments/fleet/example.yaml). Flags set on the command line override its values.")
	cmd.Flags().StringVar(&deploySSH, "ssh", "", "SSH target in user@host[:port] form (required without --spec or --local)")
	cmd.Flags().StringVar(&deploySSHKeyName, "ssh-key-name", "", "Name of an xkms-stored SSH key (private material stays in the server). Stage keys with `quicbaas ssh keys import` first.")
	cmd.Flags().StringVar(&deploySSHKeyScope, "ssh-key-scope", "", "Scope for --ssh-key-name lookup: user | tenant | service. Defaults to 'user'.")
	cmd.Flags().StringVar(&deploySSHKeyOwner, "ssh-key-owner", "", "Owner identifier for --ssh-key-name lookup (defaults to current user for scope=user, current tenant for scope=tenant, 'default' for scope=service).")
	cmd.Flags().StringVar(&deploySSHPassword, "ssh-password", "", "SSH password fallback (prefer --ssh-key-name when possible)")
	cmd.Flags().BoolVar(&deployInsecureHostKey, "ssh-insecure-host-key", false, "Skip remote host key verification (operator opt-in). Without it the target's key must be pinned in the SSH known-hosts store via `quicbaas ssh known-hosts add`, under the same scope/owner as --ssh-key-scope / --ssh-key-owner.")
	cmd.Flags().StringVar(&deployInstallDir, "install-dir", "", "Remote install directory (default ~/.quicbaas/bin)")
	cmd.Flags().StringVar(&deployDataDir, "data-dir", "", "Remote fleet node work directory (default ~/.quicbaas/agent)")
	cmd.Flags().StringVar(&deployProjectsRoot, "projects-root", "", "Remote directory the agent provisions job workspaces under (default <data-dir>/work/projects)")
	cmd.Flags().BoolVar(&deployLocal, "local", false, "Target this host with no ssh (an install made by deploy --local); cannot be combined with --ssh")

	// --ssh is not marked required: --local reaches the node without it,
	// and buildDeploySpec names it when an ssh target omits it.

	// Reject conflicting SSH auth flags at parse time. The service layer
	// also checks this invariant; rejecting here gives operators a fast,
	// clear error message.
	cmd.MarkFlagsMutuallyExclusive("ssh-key-name", "ssh-password")
}

// registerWaitOnlineFlag adds --wait-online, shared by deploy and start.
func registerWaitOnlineFlag(cmd *cobra.Command) {
	cmd.Flags().DurationVar(&deployWaitOnline, "wait-online", 2*time.Minute, "How long to wait for the agent to report online; 0 to skip")
}

// relaxRequiredFlagsForSpecFile lifts cobra's required-flag check for the
// named flags when --spec is given: the spec supplies them. Cobra validates
// required flags after PreRunE, so clearing the annotation here is enough.
func relaxRequiredFlagsForSpecFile(names ...string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		if !cmd.Flags().Changed("spec") {
			return nil
		}
		for _, name := range names {
			if err := cmd.Flags().SetAnnotation(name, cobra.BashCompOneRequiredFlag, []string{"false"}); err != nil {
				return err
			}
		}
		return nil
	}
}

// commandFlags returns cmd's flag set, or nil for a nil command (tests call
// the run functions directly).
func commandFlags(cmd *cobra.Command) *pflag.FlagSet {
	if cmd == nil {
		return nil
	}
	return cmd.Flags()
}

// deployFlagAppliers copies one explicitly-set flag onto the spec. It is how
// a flag overrides a --spec file: only flags the operator actually set are
// applied, so a flag's default never overwrites a value the spec declares.
var deployFlagAppliers = map[string]func(*deploy.Spec) error{
	"ssh": func(s *deploy.Spec) error {
		user, host, err := parseSSHTarget(deploySSH)
		if err != nil {
			return err
		}
		s.SSHUser, s.Host = user, host
		return nil
	},
	"ssh-key-name":          func(s *deploy.Spec) error { s.SSHKeyName = deploySSHKeyName; return nil },
	"ssh-key-scope":         func(s *deploy.Spec) error { s.SSHKeyScope = deploySSHKeyScope; return nil },
	"ssh-key-owner":         func(s *deploy.Spec) error { s.SSHKeyOwner = deploySSHKeyOwner; return nil },
	"ssh-password":          func(s *deploy.Spec) error { s.SSHPassword = deploySSHPassword; return nil },
	"ssh-insecure-host-key": func(s *deploy.Spec) error { s.InsecureHostKey = deployInsecureHostKey; return nil },
	"server":                func(s *deploy.Spec) error { s.Server = deployServer; return nil },
	"tenant":                func(s *deploy.Spec) error { s.Tenant = globalTenantID; return nil },
	"bind":                  func(s *deploy.Spec) error { s.NodeBind = deployBindAddr; return nil },
	"backend":               func(s *deploy.Spec) error { s.Backends = deployBackends; return nil },
	"binary":                func(s *deploy.Spec) error { s.Binary = deployBinary; return nil },
	"install-dir":           func(s *deploy.Spec) error { s.InstallDir = deployInstallDir; return nil },
	"data-dir":              func(s *deploy.Spec) error { s.DataDir = deployDataDir; return nil },
	"projects-root":         func(s *deploy.Spec) error { s.ProjectsRoot = deployProjectsRoot; return nil },
	"wait-online":           func(s *deploy.Spec) error { s.WaitOnline = deployWaitOnline; return nil },
	"max-concurrent-jobs":   func(s *deploy.Spec) error { s.Node.MaxConcurrentJobs = deployMaxJobs; return nil },
	"systemd": func(s *deploy.Spec) error {
		systemd, err := parseSystemdFlag(deploySystemdFlag)
		if err != nil {
			return err
		}
		s.Systemd = systemd
		return nil
	},
}

// resolveDeploySpec builds the spec a deploy or lifecycle command runs with.
//
// Without --spec it is the flags alone. With --spec there are three layers,
// lowest first: the flag defaults, the spec file (which replaces only the
// keys it names), and every flag set explicitly on the command line.
//
// --local then points the spec at this host. Last, a spec with no bind
// address binds the node's advertised host on the agent port, never a
// wildcard.
func resolveDeploySpec(flags *pflag.FlagSet) (*deploy.Spec, error) {
	if deployLocal && flags != nil && flags.Changed("ssh") {
		return nil, &deploy.LocalSSHConflictError{Field: "--ssh"}
	}
	spec, err := loadDeploySpec(flags)
	if err != nil {
		return nil, err
	}
	if deployLocal {
		if err := targetThisHost(spec); err != nil {
			return nil, err
		}
	} else if spec.Host == "" {
		return nil, &deploy.InvalidSpecError{Field: "host", Reason: "the spec names no host; set host, or pass --local to target this host"}
	}
	if err := spec.DefaultNodeBind(); err != nil {
		return nil, err
	}
	return spec, nil
}

// targetThisHost points spec at the host this command runs on, named by
// the advertised host of its node config (raft.raft_address). A spec that
// already names an ssh target is refused: --local and ssh are exclusive.
func targetThisHost(spec *deploy.Spec) error {
	if spec.Host != "" {
		return &deploy.LocalSSHConflictError{Field: "host"}
	}
	if err := spec.LocalSSHConflict(); err != nil {
		return err
	}
	var raft config.RaftConfig
	if cfg != nil {
		raft = cfg.Raft
	}
	host, err := raft.AdvertisedHost()
	if err != nil {
		return err
	}
	spec.Local = true
	spec.Host = host
	return nil
}

// loadDeploySpec builds the spec from the flags alone, or from the --spec
// file with the flags set on the command line laid over it.
func loadDeploySpec(flags *pflag.FlagSet) (*deploy.Spec, error) {
	if deploySpecFile == "" {
		return buildDeploySpec()
	}

	spec := deploy.DefaultSpec()
	spec.Tenant = globalTenantID
	spec.NodeBind = deployBindAddr
	spec.Backends = append([]string(nil), deployBackends...)
	spec.Binary = deployBinary
	spec.WaitOnline = deployWaitOnline
	if err := deploy.LoadSpecFile(deploySpecFile, spec); err != nil {
		return nil, err
	}

	if flags == nil {
		return spec, nil
	}
	var applyErr error
	flags.Visit(func(flag *pflag.Flag) {
		apply, ok := deployFlagAppliers[flag.Name]
		if !ok || applyErr != nil {
			return
		}
		applyErr = apply(spec)
	})
	if applyErr != nil {
		return nil, applyErr
	}
	return spec, nil
}

// formatPreflightReport prints a preflight report: one line per check, and
// under each failure the command or edit that fixes it.
func formatPreflightReport(report *deploy.PreflightReport, format string, w io.Writer) error {
	outputFmt, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFmt {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)

	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(report)

	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Preflight:\t%s\n", report.Host)
		for _, check := range report.Checks {
			if check.Passed {
				fmt.Fprintf(tw, "  ok\t%s\t%s\n", check.Name, check.Detail)
				continue
			}
			fmt.Fprintf(tw, "  FAIL\t%s\t%s\n", check.Name, check.Error)
			if check.Fix != "" {
				fmt.Fprintf(tw, "  \tfix:\t%s\n", check.Fix)
			}
		}
		verdict := "ready to deploy"
		if !report.Passed() {
			verdict = "NOT ready to deploy"
		}
		fmt.Fprintf(tw, "Result:\t%s\n", verdict)
		return tw.Flush()
	}
}
