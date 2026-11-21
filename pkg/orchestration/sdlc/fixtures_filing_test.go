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
	"time"
)

// fakeFirstFiledIssue is the number the fake gives the first issue it files,
// far from every fixture issue so a test cannot confuse the two.
const fakeFirstFiledIssue = 900

// CreateIssue files an issue in the fake, numbered after every issue it
// already holds, and records the spec so a test can assert what was written.
func (f *fakeForge) CreateIssue(_ context.Context, _ RepoRef, spec IssueSpec) (*ForgeIssue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createIssueErr != nil {
		return nil, f.createIssueErr
	}
	number := fakeFirstFiledIssue
	for existing := range f.issues {
		if existing >= number {
			number = existing + 1
		}
	}
	issue := &ForgeIssue{
		Number: number, Title: spec.Title, Body: spec.Body, State: "open",
		Labels: append([]string(nil), spec.Labels...), Created: time.Now(), Updated: time.Now(),
	}
	f.issues[number] = issue
	f.filed = append(f.filed, spec)
	copied := *issue
	return &copied, nil
}

// ListCollaborators reports the fake's configured collaborators.
func (f *fakeForge) ListCollaborators(context.Context, RepoRef) ([]ForgeUser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.collaboratorsErr != nil {
		return nil, f.collaboratorsErr
	}
	return append([]ForgeUser(nil), f.collaborators...), nil
}
