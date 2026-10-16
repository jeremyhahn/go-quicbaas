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

// Package prompt is the SDK client for a tenant's prompt sets: list a set,
// read a scope's override and its revisions, see the body a job at a scope
// runs with and why, compare it with the scope above, and edit, roll back or
// reset it (go-quicbaas#805).
package prompt

import (
	"context"
	"fmt"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// serviceName is the service the prompt client routes under.
const serviceName = "quicbaas.Prompts"

// Address names one prompt at one scope. Scope is tenant, project or system
// (read-only); empty means tenant, or project when ProjectID is set. An empty
// TenantID means the caller's own tenant.
type Address struct {
	TenantID  string `json:"tenant_id,omitempty"`
	Scope     string `json:"scope,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Set       string `json:"set"`
	Name      string `json:"name,omitempty"`
}

// ListRequest lists one set's prompts at one scope.
type ListRequest struct {
	TenantID  string `json:"tenant_id,omitempty"`
	Scope     string `json:"scope,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Set       string `json:"set"`
}

// GetRequest reads a scope's record, or one revision of it when Version is
// set.
type GetRequest struct {
	Address
	Version int64 `json:"version,omitempty"`
}

// PutRequest writes a new body at a scope. ExpectedVersion is the record's
// current version, 0 when none exists at this scope.
type PutRequest struct {
	Address
	Content         string `json:"content"`
	Note            string `json:"note"`
	ExpectedVersion int64  `json:"expected_version"`
}

// RollbackRequest writes a new revision carrying revision ToVersion's body.
type RollbackRequest struct {
	Address
	ToVersion       int64  `json:"to_version"`
	Note            string `json:"note,omitempty"`
	ExpectedVersion int64  `json:"expected_version"`
}

// ResetRequest makes a scope follow its parent again; the history is kept.
type ResetRequest struct {
	Address
	Note            string `json:"note,omitempty"`
	ExpectedVersion int64  `json:"expected_version"`
}

// VersionRef names one revision of a prompt at one layer.
type VersionRef struct {
	Set     string `json:"set"`
	Name    string `json:"name"`
	Scope   string `json:"scope"`
	ScopeID string `json:"scope_id,omitempty"`
	Version int64  `json:"version"`
	Hash    string `json:"hash"`
}

// Record is one prompt body at one scope: the current record, or one of its
// revisions.
type Record struct {
	TenantID     string    `json:"tenant_id"`
	ProjectID    string    `json:"project_id,omitempty"`
	Scope        string    `json:"scope"`
	Set          string    `json:"set"`
	Name         string    `json:"name"`
	Version      int64     `json:"version"`
	ContentHash  string    `json:"content_hash"`
	Content      string    `json:"content"`
	FollowParent bool      `json:"follow_parent"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ListEntry is one prompt of a set as a scope sees it.
type ListEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Overridden says this scope holds an edited record.
	Overridden   bool  `json:"overridden"`
	Version      int64 `json:"version,omitempty"`
	FollowParent bool  `json:"follow_parent,omitempty"`
	// Effective is the body a job at this scope runs with.
	Effective VersionRef `json:"effective"`
}

// ListResponse lists one set's prompts at one scope.
type ListResponse struct {
	Set     string      `json:"set"`
	Scope   string      `json:"scope"`
	Prompts []ListEntry `json:"prompts"`
}

// Layer is one scope of a resolution chain and what it held.
type Layer struct {
	Scope        string `json:"scope"`
	ScopeID      string `json:"scope_id,omitempty"`
	Present      bool   `json:"present"`
	Version      int64  `json:"version,omitempty"`
	Hash         string `json:"hash,omitempty"`
	FollowParent bool   `json:"follow_parent,omitempty"`
	// Answered marks the layer the body came from.
	Answered bool `json:"answered"`
}

// EffectivePrompt is the body a job at a scope runs with, where it came from
// and the whole resolution chain.
type EffectivePrompt struct {
	Set     string     `json:"set"`
	Name    string     `json:"name"`
	Scope   string     `json:"scope"`
	Content string     `json:"content"`
	Origin  VersionRef `json:"origin"`
	Chain   []Layer    `json:"chain"`
}

// Diff line operations.
const (
	DiffEqual  = "equal"
	DiffDelete = "delete"
	DiffInsert = "insert"
)

// DiffLine is one line of a diff and whether both bodies, only the parent's
// or only this scope's carry it.
type DiffLine struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

// Diff compares the body a scope runs with against the body its parent
// scope runs with.
type Diff struct {
	Set          string     `json:"set"`
	Name         string     `json:"name"`
	Scope        string     `json:"scope"`
	Origin       VersionRef `json:"origin"`
	ParentOrigin VersionRef `json:"parent_origin"`
	Same         bool       `json:"same"`
	Lines        []DiffLine `json:"lines"`
}

// RevisionSummary identifies one revision; its body is read with Get and a
// version.
type RevisionSummary struct {
	Version      int64     `json:"version"`
	ContentHash  string    `json:"content_hash"`
	Author       string    `json:"author"`
	Note         string    `json:"note,omitempty"`
	FollowParent bool      `json:"follow_parent"`
	CreatedAt    time.Time `json:"created_at"`
}

// History is a scope's record's revisions, oldest first.
type History struct {
	Set       string            `json:"set"`
	Name      string            `json:"name"`
	Scope     string            `json:"scope"`
	Revisions []RevisionSummary `json:"revisions"`
}

// Client manages a tenant's prompt sets.
type Client interface {
	// List lists a set's prompts at one scope.
	List(ctx context.Context, req *ListRequest) (*ListResponse, error)
	// Get reads the record overridden at one scope, or one of its revisions.
	Get(ctx context.Context, req *GetRequest) (*Record, error)
	// Effective resolves the body a job at one scope runs with.
	Effective(ctx context.Context, addr *Address) (*EffectivePrompt, error)
	// Diff compares a scope's body with its parent scope's.
	Diff(ctx context.Context, addr *Address) (*Diff, error)
	// History lists a record's revisions.
	History(ctx context.Context, addr *Address) (*History, error)
	// Put overrides a prompt at the tenant or project scope.
	Put(ctx context.Context, req *PutRequest) (*Record, error)
	// Rollback writes a new revision carrying an earlier revision's body.
	Rollback(ctx context.Context, req *RollbackRequest) (*Record, error)
	// Reset makes a scope follow its parent again.
	Reset(ctx context.Context, req *ResetRequest) (*Record, error)
}

// PromptsCapableTransport is implemented by a transport that serves the
// quicbaas.Prompts service. The client refuses, naming the transport, on one
// that does not.
type PromptsCapableTransport interface {
	SupportsPrompts() bool
}

type client struct {
	transport transport.Transport
}

// NewClient returns a prompt client over t, or nil when t is nil.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{transport: t}
}

// refuseUnwired fails closed on a transport that does not declare the
// Prompts service.
func (c *client) refuseUnwired(op string) error {
	if capable, ok := c.transport.(PromptsCapableTransport); ok && capable.SupportsPrompts() {
		return nil
	}
	return errors.NewPromptTransportUnsupported(op, fmt.Sprintf("%T", c.transport))
}

// checkCall runs the checks every verb shares: a wired transport and a
// context.
func (c *client) checkCall(ctx context.Context, op string) error {
	if err := c.refuseUnwired(op); err != nil {
		return err
	}
	if ctx == nil {
		return errors.NewInvalidInput(op, "context is required")
	}
	return nil
}

// checkAddress requires a set and a name.
func checkAddress(op string, addr *Address) error {
	if addr == nil {
		return errors.NewInvalidInput(op, "request is required")
	}
	if addr.Set == "" {
		return errors.NewInvalidInput(op, "set is required")
	}
	if addr.Name == "" {
		return errors.NewInvalidInput(op, "name is required")
	}
	return nil
}

// send sends one request under the Prompts service. Method is a literal at
// every call site so the MCP tool-name contract test can resolve it.
func (c *client) send(ctx context.Context, method string, req interface{}) (interface{}, error) {
	return c.transport.Request(ctx, serviceName, method, req)
}

// answer asserts the type of a verb's answer.
func answer[T any](op string, resp interface{}, err error) (*T, error) {
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*T)
	if !ok || out == nil {
		return nil, errors.NewInternal(op, "unexpected response type from server")
	}
	return out, nil
}

func (c *client) List(ctx context.Context, req *ListRequest) (*ListResponse, error) {
	const op = "prompt.List"
	if err := c.checkCall(ctx, op); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.NewInvalidInput(op, "request is required")
	}
	if req.Set == "" {
		return nil, errors.NewInvalidInput(op, "set is required")
	}
	resp, err := c.send(ctx, "List", req)
	return answer[ListResponse](op, resp, err)
}

func (c *client) Get(ctx context.Context, req *GetRequest) (*Record, error) {
	const op = "prompt.Get"
	if err := c.checkCall(ctx, op); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.NewInvalidInput(op, "request is required")
	}
	if err := checkAddress(op, &req.Address); err != nil {
		return nil, err
	}
	if req.Version < 0 {
		return nil, errors.NewInvalidInput(op, "version must not be negative")
	}
	resp, err := c.send(ctx, "Get", req)
	return answer[Record](op, resp, err)
}

func (c *client) Effective(ctx context.Context, addr *Address) (*EffectivePrompt, error) {
	const op = "prompt.Effective"
	if err := c.checkCall(ctx, op); err != nil {
		return nil, err
	}
	if err := checkAddress(op, addr); err != nil {
		return nil, err
	}
	resp, err := c.send(ctx, "Effective", addr)
	return answer[EffectivePrompt](op, resp, err)
}

func (c *client) Diff(ctx context.Context, addr *Address) (*Diff, error) {
	const op = "prompt.Diff"
	if err := c.checkCall(ctx, op); err != nil {
		return nil, err
	}
	if err := checkAddress(op, addr); err != nil {
		return nil, err
	}
	resp, err := c.send(ctx, "Diff", addr)
	return answer[Diff](op, resp, err)
}

func (c *client) History(ctx context.Context, addr *Address) (*History, error) {
	const op = "prompt.History"
	if err := c.checkCall(ctx, op); err != nil {
		return nil, err
	}
	if err := checkAddress(op, addr); err != nil {
		return nil, err
	}
	resp, err := c.send(ctx, "History", addr)
	return answer[History](op, resp, err)
}

func (c *client) Put(ctx context.Context, req *PutRequest) (*Record, error) {
	const op = "prompt.Put"
	if err := c.checkCall(ctx, op); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.NewInvalidInput(op, "request is required")
	}
	if err := checkAddress(op, &req.Address); err != nil {
		return nil, err
	}
	if req.Content == "" {
		return nil, errors.NewInvalidInput(op, "content is required")
	}
	if req.Note == "" {
		return nil, errors.NewInvalidInput(op, "note is required")
	}
	if req.ExpectedVersion < 0 {
		return nil, errors.NewInvalidInput(op, "expected_version must not be negative")
	}
	resp, err := c.send(ctx, "Put", req)
	return answer[Record](op, resp, err)
}

func (c *client) Rollback(ctx context.Context, req *RollbackRequest) (*Record, error) {
	const op = "prompt.Rollback"
	if err := c.checkCall(ctx, op); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.NewInvalidInput(op, "request is required")
	}
	if err := checkAddress(op, &req.Address); err != nil {
		return nil, err
	}
	if req.ToVersion < 1 {
		return nil, errors.NewInvalidInput(op, "to_version must be at least 1")
	}
	if req.ExpectedVersion < 1 {
		return nil, errors.NewInvalidInput(op, "expected_version must be at least 1")
	}
	resp, err := c.send(ctx, "Rollback", req)
	return answer[Record](op, resp, err)
}

func (c *client) Reset(ctx context.Context, req *ResetRequest) (*Record, error) {
	const op = "prompt.Reset"
	if err := c.checkCall(ctx, op); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.NewInvalidInput(op, "request is required")
	}
	if err := checkAddress(op, &req.Address); err != nil {
		return nil, err
	}
	if req.ExpectedVersion < 1 {
		return nil, errors.NewInvalidInput(op, "expected_version must be at least 1")
	}
	resp, err := c.send(ctx, "Reset", req)
	return answer[Record](op, resp, err)
}
