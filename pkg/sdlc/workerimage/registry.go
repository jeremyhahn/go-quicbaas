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

package workerimage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	softwarefactoryworker "github.com/jeremyhahn/go-quicbaas/deployments/agents/software-factory-worker"
)

// DefaultNPMRegistryURL is the public npm registry.
const DefaultNPMRegistryURL = "https://registry.npmjs.org"

// maxRegistryResponse bounds one registry answer. A package's "latest"
// document is a few kilobytes; anything past this is not one.
const maxRegistryResponse = 1 << 20

// defaultRegistryTimeout bounds one registry request when the caller's
// context carries no deadline of its own.
const defaultRegistryTimeout = 30 * time.Second

// VersionSource answers what the latest published version of a package is.
type VersionSource interface {
	Latest(ctx context.Context, pkg string) (string, error)
}

// NPMRegistry reads the latest versions of npm packages.
type NPMRegistry struct {
	baseURL string
	client  *http.Client
}

// NewNPMRegistry returns a registry client for baseURL, which must be an
// absolute http(s) URL. A nil client takes one with a bounded timeout.
func NewNPMRegistry(baseURL string, client *http.Client) (*NPMRegistry, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return nil, &InvalidConfigError{Field: "registry_url", Message: "must be an absolute http(s) URL"}
	}
	if client == nil {
		client = &http.Client{Timeout: defaultRegistryTimeout}
	}
	return &NPMRegistry{baseURL: strings.TrimRight(parsed.String(), "/"), client: client}, nil
}

// npmLatestDocument is the part of GET /<package>/latest this reads.
type npmLatestDocument struct {
	Version string `json:"version"`
}

// Latest returns the version the registry's "latest" dist-tag names.
//
// The answer is validated before it is returned: it becomes part of an image
// tag and a build argument, so it is only ever [0-9A-Za-z.-].
func (r *NPMRegistry) Latest(ctx context.Context, pkg string) (string, error) {
	if strings.TrimSpace(pkg) == "" {
		return "", &InvalidConfigError{Field: "package", Message: "cannot be empty"}
	}
	endpoint := r.baseURL + "/" + escapePackage(pkg) + "/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", &RegistryError{Package: pkg, Cause: err}
	}
	req.Header.Set("Accept", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return "", &RegistryError{Package: pkg, Cause: err}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", &RegistryError{Package: pkg, Status: resp.StatusCode}
	}
	var doc npmLatestDocument
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxRegistryResponse)).Decode(&doc); err != nil {
		return "", &RegistryError{Package: pkg, Cause: err}
	}
	if err := softwarefactoryworker.ValidateVersion(pkg, doc.Version); err != nil {
		return "", &RegistryError{Package: pkg, Cause: err}
	}
	return doc.Version, nil
}

// escapePackage encodes a scoped package name the way the registry routes it:
// "@scope/name" is ONE path segment, "@scope%2Fname".
func escapePackage(pkg string) string {
	return url.PathEscape(pkg)
}

// LatestAgents reads both agent CLIs' latest versions.
func LatestAgents(ctx context.Context, source VersionSource) (softwarefactoryworker.Agents, error) {
	claude, err := source.Latest(ctx, softwarefactoryworker.ClaudeCodePackage)
	if err != nil {
		return softwarefactoryworker.Agents{}, err
	}
	codex, err := source.Latest(ctx, softwarefactoryworker.CodexPackage)
	if err != nil {
		return softwarefactoryworker.Agents{}, err
	}
	return softwarefactoryworker.Agents{ClaudeCode: claude, Codex: codex}, nil
}
