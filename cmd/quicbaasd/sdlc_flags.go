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

// The sdlc flags set the SDLC module's scalar keys from the command line.
// The list-valued and map-valued sdlc keys (repo_hosts, rulers, triage_agent,
// worker_image, surfacing, label_reconcile, forge) have no flag: they stay
// file-only or env-only, as the configuration reference says.

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// sdlcFlagKeys maps each sdlc flag to the config key it sets.
var sdlcFlagKeys = map[string]string{
	"sdlc-enabled":            "sdlc.enabled",
	"sdlc-issue-page-limit":   "sdlc.issue_page_limit",
	"sdlc-checkout-root":      "sdlc.checkout_root",
	"sdlc-merge-attempts":     "sdlc.merge_attempts",
	"sdlc-webhook-max-body":   "sdlc.webhook.max_body",
	"sdlc-webhook-public-url": "sdlc.webhook.public_url",
}

// sdlcFlags holds the sdlc flags. The start, node and cluster commands share
// its flag values, so one viper binding per key serves whichever command
// parsed them.
var sdlcFlags = newSDLCFlags()

// newSDLCFlags builds the sdlc flag set. Each default is the defaults-layer
// value, so an unset flag never masks a lower layer with a different value.
func newSDLCFlags() *pflag.FlagSet {
	defaults := config.Default().SDLC
	fs := pflag.NewFlagSet("sdlc", pflag.ContinueOnError)
	fs.Bool("sdlc-enabled", defaults.Enabled,
		"Run the SDLC module on this node (sdlc.enabled, env QUICBAAS_SDLC_ENABLED)")
	fs.Int("sdlc-issue-page-limit", defaults.IssuePageLimit,
		"Issues the SDLC module reads per forge page (sdlc.issue_page_limit, env QUICBAAS_SDLC_ISSUE_PAGE_LIMIT)")
	fs.String("sdlc-checkout-root", defaults.CheckoutRoot,
		"Directory the SDLC module checks repositories out under (sdlc.checkout_root, env QUICBAAS_SDLC_CHECKOUT_ROOT)")
	fs.Int("sdlc-merge-attempts", defaults.MergeAttempts,
		"Merge attempts before the SDLC module gives up on a pull request (sdlc.merge_attempts, env QUICBAAS_SDLC_MERGE_ATTEMPTS)")
	fs.Int64("sdlc-webhook-max-body", defaults.Webhook.MaxBody,
		"Largest forge webhook body accepted, in bytes (sdlc.webhook.max_body, env QUICBAAS_SDLC_WEBHOOK_MAX_BODY)")
	fs.String("sdlc-webhook-public-url", defaults.Webhook.PublicURL,
		"Public URL forges deliver webhooks to, empty = poll-only (sdlc.webhook.public_url, env QUICBAAS_SDLC_WEBHOOK_PUBLIC_URL)")
	return fs
}

// addSDLCFlags registers the sdlc flags on each command.
func addSDLCFlags(cmds ...*cobra.Command) {
	for _, cmd := range cmds {
		cmd.Flags().AddFlagSet(sdlcFlags)
	}
}

// bindSDLCKeys binds every sdlc flag to its key on v. bindEnvVars binds the
// environment names, so v resolves each key as flag > env > file > defaults.
func bindSDLCKeys(v *viper.Viper) error {
	for name, key := range sdlcFlagKeys {
		if err := v.BindPFlag(key, sdlcFlags.Lookup(name)); err != nil {
			return api.NewFlagBindError("quicbaasd.bindSDLCKeys", name, err)
		}
	}
	return nil
}
