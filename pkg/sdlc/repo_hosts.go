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

package sdlc

import (
	"net/url"
	"strings"
)

// =============================================================================
// RepoHostGuard — the operator's allowlist of forge hosts
// =============================================================================
//
// A project's repository URL is tenant data that directs the daemon's git at
// a network host, so only a host the operator named in sdlc.repo_hosts is
// accepted. This is the repository half of the execution policy; the command
// half (which verification commands a workflow may run) is orchestration's
// ExecutionGuard, because it names no repository.

// RepoHostGuard decides which repository URLs the module will clone.
type RepoHostGuard struct {
	hosts           map[string]struct{}
	allowLocalRepos bool
}

// NewRepoHostGuard builds the guard from the operator's host list. Hosts
// are matched case-insensitively by hostname or host:port; allowLocalRepos
// additionally admits file:// and bare-path repositories.
func NewRepoHostGuard(hosts []string, allowLocalRepos bool) (RepoHostGuard, error) {
	g := RepoHostGuard{hosts: make(map[string]struct{}, len(hosts)), allowLocalRepos: allowLocalRepos}
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" {
			return RepoHostGuard{}, &InvalidInputError{
				Field:   "sdlc.repo_hosts",
				Message: "an empty allowlist entry would match no host and hide a typo",
			}
		}
		g.hosts[h] = struct{}{}
	}
	return g, nil
}

// CheckRepoURL reports whether repoURL points at an allowed host.
func (g RepoHostGuard) CheckRepoURL(repoURL string) error {
	raw := strings.TrimSpace(repoURL)
	if raw == "" {
		return &RepoURLRefusedError{RepoURL: repoURL, Reason: "empty"}
	}
	if !strings.Contains(raw, "://") {
		if at := strings.Index(raw, "@"); at > 0 {
			if host, _, scpForm := strings.Cut(raw[at+1:], ":"); scpForm {
				return g.checkHost(raw, host, "")
			}
		}
		if g.allowLocalRepos {
			return nil
		}
		return &RepoURLRefusedError{RepoURL: raw, Reason: "a bare path directs the daemon's git at its own filesystem"}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return &RepoURLRefusedError{RepoURL: raw, Reason: "unparseable"}
	}
	switch u.Scheme {
	case "ssh", "https":
		return g.checkHost(raw, u.Hostname(), strings.ToLower(u.Host))
	case "file":
		if g.allowLocalRepos {
			return nil
		}
		return &RepoURLRefusedError{RepoURL: raw, Reason: "a file:// repository directs the daemon's git at its own filesystem"}
	default:
		return &RepoURLRefusedError{RepoURL: raw, Reason: "scheme " + u.Scheme + " is not a supported git transport"}
	}
}

func (g RepoHostGuard) checkHost(raw, hostname, hostport string) error {
	hostname = strings.ToLower(strings.TrimSpace(hostname))
	if hostname == "" {
		return &RepoURLRefusedError{RepoURL: raw, Reason: "no host"}
	}
	if _, ok := g.hosts[hostname]; ok {
		return nil
	}
	if hostport != "" {
		if _, ok := g.hosts[hostport]; ok {
			return nil
		}
	}
	return &RepoURLRefusedError{RepoURL: raw, Reason: "host " + hostname + " is not an allowed forge host"}
}
