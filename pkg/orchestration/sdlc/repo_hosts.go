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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
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
	case "http":
		return &RepoURLRefusedError{
			RepoURL: raw,
			Reason:  "http:// carries no credential -- the forge token is never sent in cleartext, so the worker could clone only a public repository and would fail on a private one; register the https:// or ssh:// URL",
		}
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

// CheckProject reports whether the worker can clone project: its own
// repository and every context repository, under the same rules bind applies.
//
// It is called at bind AND every time a workspace is opened. The project
// record is editable after it is bound -- a context repository added, a
// remote moved -- and the operator's allowlist can shrink under it, so a
// check made once at bind proves nothing about the checkout a later job
// asks for (go-quicbaas#770).
func (g RepoHostGuard) CheckProject(project *orchestration.Project) error {
	if err := g.CheckRepoURL(project.Git.RepoURL); err != nil {
		return err
	}
	return g.CheckContextRepos(project.Git.RepoURL, project.ContextRepos)
}

// configured reports whether the guard was built by NewRepoHostGuard. The
// zero value admits no host at all, so a runner handed one would refuse
// every workspace; it is refused at construction instead.
func (g RepoHostGuard) configured() bool { return g.hosts != nil }

// CheckContextRepos reports whether the worker can clone every context
// repository of a project whose own repository is projectRepoURL.
//
// A context repository is cloned by the same container, with the same
// credentials, as the project, so it answers to the same allowlist -- and to
// one more rule: the spawn pins exactly one ssh host key, the binding's, for
// the project's ssh host. A context repository on any other ssh host would
// fail host-key verification inside the worker, so it is refused here, where
// the operator reads the reason, rather than three attempts later in a
// container nobody can see into (go-quicbaas#770).
func (g RepoHostGuard) CheckContextRepos(projectRepoURL string, repos []orchestration.ContextRepo) error {
	projectHost := orchestration.SSHRepoHost(projectRepoURL)
	for _, repo := range repos {
		if err := g.CheckRepoURL(repo.RepoURL); err != nil {
			return &ContextRepoRefusedError{Name: repo.Name, Err: err}
		}
		host := orchestration.SSHRepoHost(repo.RepoURL)
		if host == "" || host == projectHost {
			continue
		}
		pinned := "the project is not cloned over ssh, so no host key is pinned at all"
		if projectHost != "" {
			pinned = "the worker pins only the project's ssh host, " + projectHost
		}
		return &ContextRepoRefusedError{
			Name: repo.Name,
			Err: &InvalidInputError{
				Field:   "repo_url",
				Message: "ssh host " + host + " cannot be verified: " + pinned + "; register its https:// URL, or host it beside the project",
			},
		}
	}
	return nil
}
