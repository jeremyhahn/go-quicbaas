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

package sdlc

import (
	"net/url"
	"path"
	"strings"
)

// ModulePolicy says how a project's PRIVATE Go modules are reached from the
// worker's container.
//
// THE PROBLEM IT SOLVES. A module is named by its import path
// (github.com/<owner>/<repo>) and, for a private module, is fetched with git
// from wherever the code actually lives — here, the forge. `go build` in a
// fresh container asks the public proxy for github.com/<owner>/<repo>, is told
// it does not exist, and the whole verification fails before one line of the
// worker's change compiles. Measured on go-quicbaas#53 (2026-09-21): three
// review rounds of an identical dependency-resolution failure, escalated as if
// the work were at fault. The first two factory projects never hit it because
// neither depends on a private module.
//
// THE MECHANISM IS THE ONE THE HANDBOOK PRESCRIBES for containers
// (dependency-resolution.md §6): GOPRIVATE for the owner's modules, so the
// proxy and the checksum database are bypassed, and a git url rewrite from the
// import-path prefix to the forge. The rewrite here is to SSH, not to a
// tokenised HTTP URL: the container already holds the project's access key in
// its ssh-agent and the forge's pinned host key in its known_hosts, so
// nothing secret has to travel in the environment, and the same key that
// clones the project reads its siblings.
type ModulePolicy struct {
	// RewritePrefix is the import-path prefix, written as an https URL with
	// a trailing slash, whose modules live on this project's forge under the
	// same owner: "https://github.com/jeremyhahn/". Empty means the project
	// has no private modules and nothing is injected.
	RewritePrefix string `json:"rewrite_prefix,omitempty"`
}

// Validate refuses a prefix that is not an https URL naming a host and one
// owner path, with a trailing slash — the shape git's insteadOf matches on.
func (p ModulePolicy) Validate() error {
	prefix := strings.TrimSpace(p.RewritePrefix)
	if prefix == "" {
		return nil
	}
	u, err := url.Parse(prefix)
	if err != nil || u.Scheme != "https" || u.Host == "" || !strings.HasSuffix(u.Path, "/") ||
		strings.Count(strings.Trim(u.Path, "/"), "/") != 0 || strings.Trim(u.Path, "/") == "" {
		return &InvalidInputError{
			Field:   "modules.rewrite_prefix",
			Message: "must be an https URL of the form https://<host>/<owner>/, the import-path prefix the forge hosts",
		}
	}
	return nil
}

// GoPrivate is the GOPRIVATE pattern for the prefix: "github.com/jeremyhahn/*".
func (p ModulePolicy) GoPrivate() string {
	prefix := strings.TrimSpace(p.RewritePrefix)
	if prefix == "" {
		return ""
	}
	u, err := url.Parse(prefix)
	if err != nil {
		return ""
	}
	return u.Host + "/" + strings.Trim(u.Path, "/") + "/*"
}

// ModuleEnv is the environment the worker's container needs to resolve the
// project's private modules, or nil when the policy names none.
//
// The rewrite target is derived from the project's own repository URL — the
// same scheme, host, port and owner, with the repository name removed — so a
// project reached over ssh://git@forge:2022/owner/repo.git has its siblings
// reached at ssh://git@forge:2022/owner/. A repository URL the derivation
// cannot read is an error, not a silent no-op: a worker that then fails
// verification would be charged for a misconfiguration.
func ModuleEnv(policy ModulePolicy, repoURL string) (map[string]string, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(policy.RewritePrefix) == "" {
		return nil, nil
	}
	u, err := url.Parse(strings.TrimSpace(repoURL))
	if err != nil || u.Scheme == "" || u.Host == "" || strings.Trim(u.Path, "/") == "" {
		return nil, &InvalidInputError{
			Field:   "project.git_repo_url",
			Message: "the module rewrite target is derived from the repository URL, which must name a scheme, host and owner/repo path",
		}
	}
	owner := path.Dir(strings.Trim(u.Path, "/"))
	if owner == "." || owner == "" {
		return nil, &InvalidInputError{
			Field:   "project.git_repo_url",
			Message: "the repository URL carries no owner segment to rewrite sibling modules under",
		}
	}
	target := u.Scheme + "://"
	if u.User != nil {
		target += u.User.String() + "@"
	}
	target += u.Host + "/" + owner + "/"
	return map[string]string{
		"GOPRIVATE":          policy.GoPrivate(),
		"GIT_CONFIG_COUNT":   "1",
		"GIT_CONFIG_KEY_0":   "url." + target + ".insteadOf",
		"GIT_CONFIG_VALUE_0": strings.TrimSpace(policy.RewritePrefix),
	}, nil
}
