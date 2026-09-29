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
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// FileIssueRequest files one issue on a bound project's repository.
type FileIssueRequest struct {
	TenantID  string `json:"tenant_id,omitempty" yaml:"tenant_id,omitempty"`
	ProjectID string `json:"project_id" yaml:"project_id"`

	// Title is required: one line, at most 255 characters.
	Title string `json:"title" yaml:"title"`

	// Body is the issue's description.
	Body string `json:"body,omitempty" yaml:"body,omitempty"`

	// Labels are label names the repository already defines, e.g.
	// task:ready to hand the issue straight to the factory.
	Labels []string `json:"labels,omitempty" yaml:"labels,omitempty"`

	// Parent is the issue this one was split out of; zero for none.
	Parent int `json:"parent,omitempty" yaml:"parent,omitempty"`
}

// ForgeIssue is an issue as the forge stored it.
type ForgeIssue struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	Body   string   `json:"body,omitempty"`
	State  string   `json:"state,omitempty"`
	Labels []string `json:"labels,omitempty"`
	Author string   `json:"author,omitempty"`
	URL    string   `json:"url,omitempty"`
}

// CollaboratorsRequest reads the accounts that may rule on a bound project's
// repository.
type CollaboratorsRequest struct {
	TenantID  string `json:"tenant_id,omitempty" yaml:"tenant_id,omitempty"`
	ProjectID string `json:"project_id" yaml:"project_id"`
}

// ForgeUser is one account with access to a repository.
type ForgeUser struct {
	Login    string `json:"login"`
	FullName string `json:"full_name,omitempty"`
}

// CollaboratorsResponse lists the repository's owner first, then every
// collaborator.
type CollaboratorsResponse struct {
	Users []ForgeUser `json:"users"`
	Count int         `json:"count"`
}

// FileIssue implements Client.
func (c *client) FileIssue(ctx context.Context, req *FileIssueRequest) (*ForgeIssue, error) {
	const op = "sdlc.FileIssue"
	if err := c.refuseUnwired(op); err != nil {
		return nil, err
	}
	switch {
	case ctx == nil:
		return nil, errors.NewInvalidInput(op, "context is required")
	case req == nil:
		return nil, errors.NewInvalidInput(op, "request is required")
	case req.ProjectID == "":
		return nil, errors.NewInvalidInput(op, "project_id is required")
	case strings.TrimSpace(req.Title) == "":
		return nil, errors.NewInvalidInput(op, "title is required")
	case req.Parent < 0:
		return nil, errors.NewInvalidInput(op, "parent must be a positive issue number, or zero for none")
	}
	resp, err := c.transport.Request(ctx, serviceName, "FileIssue", req)
	if err != nil {
		return nil, err
	}
	issue, ok := resp.(*ForgeIssue)
	if !ok || issue == nil {
		return nil, errors.NewInternal(op, "unexpected response type from server")
	}
	return issue, nil
}

// Collaborators implements Client.
func (c *client) Collaborators(ctx context.Context, tenantID, projectID string) (*CollaboratorsResponse, error) {
	const op = "sdlc.Collaborators"
	if err := c.refuseUnwired(op); err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, errors.NewInvalidInput(op, "context is required")
	}
	if projectID == "" {
		return nil, errors.NewInvalidInput(op, "project_id is required")
	}
	resp, err := c.transport.Request(ctx, serviceName, "Collaborators", &CollaboratorsRequest{TenantID: tenantID, ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	users, ok := resp.(*CollaboratorsResponse)
	if !ok || users == nil {
		return nil, errors.NewInternal(op, "unexpected response type from server")
	}
	return users, nil
}
