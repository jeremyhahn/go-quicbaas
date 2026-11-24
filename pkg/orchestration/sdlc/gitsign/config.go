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

package gitsign

import (
	"net/url"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// Environment the helper reads. git runs gpg.ssh.program with no arguments of
// quicbaas's choosing, so everything the helper needs beyond git's own flags
// comes from the environment of the git command. The mTLS material is the
// session's own MCP client certificate, under the names every MCP client in a
// worker already reads.
const (
	// EnvMCPURL is the quicbaas MCP endpoint, e.g. https://host:8443/mcp.
	EnvMCPURL = "QUICBAAS_GIT_SIGN_MCP_URL"

	// EnvKeyName, EnvKeyScope and EnvKeyOwner name the session's SSH key in
	// quicbaas. Only the name is required; an empty scope or owner takes the
	// server's default for the session's tenant.
	EnvKeyName  = "QUICBAAS_GIT_SIGN_KEY_NAME"
	EnvKeyScope = "QUICBAAS_GIT_SIGN_KEY_SCOPE"
	EnvKeyOwner = "QUICBAAS_GIT_SIGN_KEY_OWNER"
)

// KeyRef names the session's SSH key in quicbaas.
type KeyRef struct {
	Name  string
	Scope string
	Owner string
}

// Config is where the helper reaches quicbaas and which key it signs with.
type Config struct {
	MCPURL   string
	CertFile string
	KeyFile  string
	CAFile   string
	Key      KeyRef
}

// requiredVariables are the variables ConfigFromEnv refuses to run without.
var requiredVariables = []string{
	EnvMCPURL,
	orchestration.MCPCertEnvVar,
	orchestration.MCPKeyEnvVar,
	orchestration.MCPCAEnvVar,
	EnvKeyName,
}

// ConfigFromEnv reads the helper's configuration through getenv. It returns an
// *InvalidConfigError naming the first required variable that is unset, or an
// MCP URL that is not https.
func ConfigFromEnv(getenv func(string) string) (*Config, error) {
	for _, name := range requiredVariables {
		if getenv(name) == "" {
			return nil, &InvalidConfigError{Variable: name, Message: "not set"}
		}
	}
	endpoint, err := url.Parse(getenv(EnvMCPURL))
	if err != nil {
		return nil, &InvalidConfigError{Variable: EnvMCPURL, Message: "not a URL", Cause: err}
	}
	if endpoint.Scheme != "https" || endpoint.Host == "" {
		return nil, &InvalidConfigError{Variable: EnvMCPURL, Message: "must be an https URL with a host"}
	}
	return &Config{
		MCPURL:   endpoint.String(),
		CertFile: getenv(orchestration.MCPCertEnvVar),
		KeyFile:  getenv(orchestration.MCPKeyEnvVar),
		CAFile:   getenv(orchestration.MCPCAEnvVar),
		Key: KeyRef{
			Name:  getenv(EnvKeyName),
			Scope: getenv(EnvKeyScope),
			Owner: getenv(EnvKeyOwner),
		},
	}, nil
}
