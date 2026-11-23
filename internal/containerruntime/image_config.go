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

package containerruntime

import (
	"maps"
	"slices"
	"strings"
)

// withImageDefaults returns a copy of config with what it leaves unset taken
// from the image's configuration, as `docker run` does:
//
//   - the process is the entrypoint followed by the command. An unset (nil)
//     entrypoint is the image's, and then an unset command is the image's
//     too; a set entrypoint, including an explicitly empty one that clears
//     the image's, drops the image's command;
//   - the image's environment is the base the container's overrides;
//   - an unset working directory or user is the image's;
//   - the image's labels are the base the container's labels override.
//
// The copy's Entrypoint is folded into Command, the one argv the OCI
// configuration is built from.
func withImageDefaults(config *ContainerConfig, image imageConfig) *ContainerConfig {
	resolved := *config

	entrypoint, cmd := config.Entrypoint, config.Command
	if entrypoint == nil {
		entrypoint = image.Entrypoint
		if len(cmd) == 0 {
			cmd = image.Cmd
		}
	}
	resolved.Entrypoint = nil
	resolved.Command = append(slices.Clone(entrypoint), cmd...)
	if len(resolved.Command) == 0 {
		resolved.Command = nil
	}

	if len(image.Env) > 0 {
		env := make(map[string]string, len(image.Env)+len(config.Environment))
		for _, entry := range image.Env {
			key, value, _ := strings.Cut(entry, "=")
			if key != "" {
				env[key] = value
			}
		}
		maps.Copy(env, config.Environment)
		resolved.Environment = env
	}

	if resolved.WorkDir == "" {
		resolved.WorkDir = image.WorkingDir
	}
	if resolved.User == "" {
		resolved.User = image.User
	}

	if len(image.Labels) > 0 {
		labels := maps.Clone(image.Labels)
		maps.Copy(labels, config.Labels)
		resolved.Labels = labels
	}
	return &resolved
}
