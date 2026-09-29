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
	"reflect"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet"
)

// serveRequiredFlags are the `fleet serve` flags a flag-only invocation must
// supply. With --config the same settings may come from the file instead, so
// cobra's required check is relaxed and fleet.Config.Validate — which runs on
// the merged result either way — is what refuses a missing one.
var serveRequiredFlags = []string{"server", "tenant", "node-cert", "node-key", "server-ca"}

// serveFlagFields maps every `fleet serve` flag to the fleet.Config field it
// sets. It is how an explicitly-set flag overrides the config file: the flag's
// already-parsed value is copied over the file's for exactly that field.
//
// Every flag except --config itself must appear here; a flag missing from the
// table would be silently ignored whenever --config is given, which
// TestServeFlagFields_CoverEveryFlag refuses.
var serveFlagFields = map[string]string{
	"server":                      "ServerAddr",
	"tenant":                      "TenantID",
	"bind":                        "BindAddr",
	"backends":                    "Backends",
	"sandbox-policy":              "SandboxPolicy",
	"sandbox-network":             "SandboxNetwork",
	"sandbox-cap-add":             "SandboxCapAdd",
	"sandbox-pids-limit":          "SandboxPidsLimit",
	"sandbox-ulimit":              "SandboxUlimits",
	"container-runtime":           "ContainerRuntime",
	"ollama-url":                  "OllamaBaseURL",
	"openvino-url":                "OpenVINOBaseURL",
	"openvino-model":              "OpenVINOModel",
	"openvino-context-tokens":     "OpenVINOContextTokens",
	"ollama-agent-model":          "OllamaAgentModel",
	"ollama-agent-context-tokens": "OllamaAgentContextTokens",
	"vllm-url":                    "VLLMBaseURL",
	"vllm-model":                  "VLLMModel",
	"vllm-context-tokens":         "VLLMContextTokens",
	"codex-reasoning-effort":      "CodexReasoningEffort",
	"image-map":                   "ImageMap",
	"image-registry":              "ImageRegistry",
	"auto-pull":                   "AutoPull",
	"work-dir":                    "WorkDir",
	"projects-root":               "ProjectsRoot",
	"sockets-root":                "SocketsRoot",
	"go-cache-dir":                "GoCacheDir",
	"node-cert":                   "NodeCertFile",
	"node-key":                    "NodeKeyFile",
	"server-ca":                   "ServerCAFile",
	"heartbeat-interval":          "HeartbeatInterval",
	"shutdown-grace":              "ShutdownGrace",
	"enroll-retry-base":           "EnrollRetryBase",
	"enroll-retry-max":            "EnrollRetryMax",
	"allowed-mount-root":          "AllowedMountRoots",
	"mcp-endpoint":                "MCPEndpoint",
	"max-concurrent-jobs":         "MaxConcurrentJobs",
}

// relaxRequiredServeFlagsForConfigFile lifts cobra's required-flag check when
// --config is given. Cobra validates required flags after PreRunE, so clearing
// the annotation here is what lets `fleet serve --config <file>` run with no
// other flag at all.
func relaxRequiredServeFlagsForConfigFile(cmd *cobra.Command, _ []string) error {
	if !cmd.Flags().Changed("config") {
		return nil
	}
	for _, name := range serveRequiredFlags {
		if err := cmd.Flags().SetAnnotation(name, cobra.BashCompOneRequiredFlag, []string{"false"}); err != nil {
			return err
		}
	}
	return nil
}

// resolveServeConfig builds the daemon's Config from three layers, lowest
// first:
//
//  1. the flag defaults,
//  2. the --config file, which replaces only the keys it names,
//  3. every flag set explicitly on the command line.
//
// Without --config it is exactly the flag-built config. The result is not
// validated here; runFleetServe validates the merged config once.
func resolveServeConfig(flags *pflag.FlagSet) (*fleet.Config, error) {
	fromFlags, err := buildServeConfig()
	if err != nil {
		return nil, err
	}
	if serveConfigFile == "" {
		return fromFlags, nil
	}

	// A second build gives the file its own copies of the default slices and
	// maps, so decoding into it cannot reach back into fromFlags.
	cfg, err := buildServeConfig()
	if err != nil {
		return nil, err
	}
	if err := fleet.LoadConfigFile(serveConfigFile, cfg); err != nil {
		return nil, err
	}

	dst := reflect.ValueOf(cfg).Elem()
	src := reflect.ValueOf(fromFlags).Elem()
	var applyErr error
	flags.Visit(func(flag *pflag.Flag) {
		field, ok := serveFlagFields[flag.Name]
		if !ok {
			return
		}
		target := dst.FieldByName(field)
		if !target.IsValid() {
			applyErr = &MessageError{Message: "agent serve: flag --" + flag.Name + " maps to unknown config field " + field}
			return
		}
		target.Set(src.FieldByName(field))
	})
	if applyErr != nil {
		return nil, applyErr
	}
	return cfg, nil
}
