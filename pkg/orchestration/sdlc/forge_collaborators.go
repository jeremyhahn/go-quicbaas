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
	"context"
	"sort"
	"strings"
)

// ForgeUser is one forge account as a repository's collaborator listing
// reports it.
type ForgeUser struct {
	// Login is the account's forge login, the value a ruler names.
	Login string `json:"login"`

	// FullName is the account's display name; empty when it set none.
	FullName string `json:"full_name,omitempty"`
}

// ForgeCollaborators is the people-level surface: who may act on a
// repository. ForgeBackend embeds it, so an adapter stays complete or
// unregistered.
type ForgeCollaborators interface {
	// ListCollaborators returns every account granted access to the
	// repository, the owner excluded.
	//
	// WHOLE, paginated by the adapter, and a prefix is a fault: the listing
	// is what a binding's rulers are validated against, so a truncated one
	// would refuse a ruler the repository plainly grants.
	ListCollaborators(ctx context.Context, repo RepoRef) ([]ForgeUser, error)
}

// Collaborators returns the accounts that may rule on a bound repository:
// its owner first, then every collaborator sorted by login, each once.
func Collaborators(ctx context.Context, forge ForgeBackend, repo RepoRef) ([]ForgeUser, error) {
	if forge == nil {
		return nil, &InvalidInputError{Field: "forge", Message: "collaborators are read through the binding's forge adapter"}
	}
	listed, err := forge.ListCollaborators(ctx, repo)
	if err != nil {
		return nil, err
	}
	sort.Slice(listed, func(i, j int) bool {
		return strings.ToLower(listed[i].Login) < strings.ToLower(listed[j].Login)
	})
	users := make([]ForgeUser, 0, len(listed)+1)
	seen := make(map[string]struct{}, len(listed)+1)
	add := func(user ForgeUser) {
		key := strings.ToLower(strings.TrimSpace(user.Login))
		if key == "" {
			return
		}
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		users = append(users, user)
	}
	add(ForgeUser{Login: repo.Owner})
	for _, user := range listed {
		add(user)
	}
	return users, nil
}

// CheckRulers refuses a binding ruler who is neither the repository's owner
// nor one of its collaborators. users is the Collaborators answer.
//
// A RULER CLEARS AN OWNER DECISION, so a login the owner never granted access
// to the repository is nobody the owner delegated that decision to. Refused
// at bind time, with the login named, rather than accepted and silently never
// matching the comment its holder cannot post.
func CheckRulers(rulers []string, users []ForgeUser) error {
	granted := make(map[string]struct{}, len(users))
	for _, user := range users {
		granted[strings.ToLower(strings.TrimSpace(user.Login))] = struct{}{}
	}
	for _, login := range rulers {
		if _, ok := granted[strings.ToLower(strings.TrimSpace(login))]; !ok {
			return &RulerNotCollaboratorError{Login: login}
		}
	}
	return nil
}
