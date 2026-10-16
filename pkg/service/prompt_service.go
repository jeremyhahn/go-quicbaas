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

package service

import (
	"context"
	"errors"
	"strings"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// PromptProjectLookup is the one ProjectService verb the prompt service
// needs: proving a project-scope address names a project of the tenant.
type PromptProjectLookup interface {
	GetProject(ctx context.Context, req *GetProjectRequest) (*GetProjectResponse, error)
}

// PromptServiceConfig is what a PromptService is built over.
type PromptServiceConfig struct {
	// Store reads and writes prompt records. Required.
	Store *prompts.Store

	// Resolver chooses the body a job runs with. Required.
	Resolver *prompts.Resolver

	// Projects proves project-scope addresses. Optional: without it a
	// project id is taken as given.
	Projects PromptProjectLookup
}

// PromptService serves the prompt verbs every transport exposes: read a
// scope's override, see what a job would run with and why, compare it with
// the scope above, and edit, roll back or reset it. The system scope is
// read-only; only the boot reconciler writes it.
type PromptService struct {
	store    *prompts.Store
	resolver *prompts.Resolver
	projects PromptProjectLookup
}

// PromptAddress names one prompt at one scope. Scope is tenant, project or
// system; empty means project when ProjectID is set and tenant otherwise.
type PromptAddress struct {
	TenantID  string `json:"tenant_id"`
	Scope     string `json:"scope,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Set       string `json:"set"`
	Name      string `json:"name"`
}

// ListPromptsRequest lists one set's prompts at one scope.
type ListPromptsRequest struct {
	TenantID  string `json:"tenant_id"`
	Scope     string `json:"scope,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Set       string `json:"set"`
}

// GetPromptRequest reads a scope's record, or one revision of it when
// Version is set.
type GetPromptRequest struct {
	PromptAddress
	Version int64 `json:"version,omitempty"`
}

// PutPromptRequest writes a new body at a scope. ExpectedVersion is the
// version the caller read, 0 to create; Note says why and is required.
type PutPromptRequest struct {
	PromptAddress
	Content         string `json:"content"`
	Note            string `json:"note"`
	ExpectedVersion int64  `json:"expected_version"`
}

// RollbackPromptRequest writes an earlier revision's body as a new revision.
type RollbackPromptRequest struct {
	PromptAddress
	ToVersion       int64  `json:"to_version"`
	Note            string `json:"note,omitempty"`
	ExpectedVersion int64  `json:"expected_version"`
}

// ResetPromptRequest returns a scope's record to following its parent.
type ResetPromptRequest struct {
	PromptAddress
	Note            string `json:"note,omitempty"`
	ExpectedVersion int64  `json:"expected_version"`
}

// PromptView is one prompt body at one scope: the current record, or one
// revision of it.
type PromptView struct {
	TenantID     string          `json:"tenant_id"`
	ProjectID    string          `json:"project_id,omitempty"`
	Scope        prompts.Scope   `json:"scope"`
	Set          prompts.SetKind `json:"set"`
	Name         string          `json:"name"`
	Version      int64           `json:"version"`
	ContentHash  string          `json:"content_hash"`
	Content      string          `json:"content"`
	FollowParent bool            `json:"follow_parent"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// PromptListEntry is one prompt of a set as a scope sees it.
type PromptListEntry struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Overridden says this scope holds an edited record: one that does not
	// follow its parent.
	Overridden bool `json:"overridden"`

	// Version and FollowParent describe the record at this scope; Version is
	// 0 when there is none. Version is the expected_version of the next Put.
	Version      int64 `json:"version,omitempty"`
	FollowParent bool  `json:"follow_parent,omitempty"`

	// Effective is the body a job at this scope runs with.
	Effective prompts.VersionRef `json:"effective"`
}

// ListPromptsResponse lists one set's prompts at one scope.
type ListPromptsResponse struct {
	Set     prompts.SetKind   `json:"set"`
	Scope   prompts.Scope     `json:"scope"`
	Prompts []PromptListEntry `json:"prompts"`
}

// PromptLayer is one layer of the resolution chain.
type PromptLayer struct {
	Scope   prompts.Scope `json:"scope"`
	ScopeID string        `json:"scope_id,omitempty"`

	// Present says the layer holds a record (always true for embedded).
	Present      bool   `json:"present"`
	Version      int64  `json:"version,omitempty"`
	Hash         string `json:"hash,omitempty"`
	FollowParent bool   `json:"follow_parent,omitempty"`

	// Answered marks the layer the effective body came from.
	Answered bool `json:"answered"`
}

// EffectivePrompt is the body a job at a scope runs with, where it came
// from, and every layer resolution looked at, nearest first.
type EffectivePrompt struct {
	Set     prompts.SetKind    `json:"set"`
	Name    string             `json:"name"`
	Scope   prompts.Scope      `json:"scope"`
	Content string             `json:"content"`
	Origin  prompts.VersionRef `json:"origin"`
	Chain   []PromptLayer      `json:"chain"`
}

// PromptDiffOp is what a diff line does to the parent's body.
type PromptDiffOp string

const (
	// PromptDiffEqual is a line both bodies carry.
	PromptDiffEqual PromptDiffOp = "equal"

	// PromptDiffDelete is a line only the parent's body carries.
	PromptDiffDelete PromptDiffOp = "delete"

	// PromptDiffInsert is a line only this scope's body carries.
	PromptDiffInsert PromptDiffOp = "insert"
)

// PromptDiffLine is one line of a diff.
type PromptDiffLine struct {
	Op   PromptDiffOp `json:"op"`
	Text string       `json:"text"`
}

// PromptDiff compares the body a scope runs with against the body its
// parent scope runs with; the system scope's parent is the embedded default.
type PromptDiff struct {
	Set          prompts.SetKind    `json:"set"`
	Name         string             `json:"name"`
	Scope        prompts.Scope      `json:"scope"`
	Origin       prompts.VersionRef `json:"origin"`
	ParentOrigin prompts.VersionRef `json:"parent_origin"`
	Same         bool               `json:"same"`
	Lines        []PromptDiffLine   `json:"lines"`
}

// PromptRevisionSummary identifies one revision. History carries no
// bodies: prompts:history grants the log, not the content.
type PromptRevisionSummary struct {
	Version      int64     `json:"version"`
	ContentHash  string    `json:"content_hash"`
	Author       string    `json:"author"`
	Note         string    `json:"note,omitempty"`
	FollowParent bool      `json:"follow_parent"`
	CreatedAt    time.Time `json:"created_at"`
}

// PromptHistory is a scope's record's revisions, oldest first.
type PromptHistory struct {
	Set       prompts.SetKind         `json:"set"`
	Name      string                  `json:"name"`
	Scope     prompts.Scope           `json:"scope"`
	Revisions []PromptRevisionSummary `json:"revisions"`
}

// promptScopes are the scopes a request may name. Embedded is a layer of
// resolution, not an address.
var promptScopes = map[string]prompts.Scope{
	string(prompts.ScopeTenant):  prompts.ScopeTenant,
	string(prompts.ScopeProject): prompts.ScopeProject,
	string(prompts.ScopeSystem):  prompts.ScopeSystem,
}

// NewPromptService builds the prompt service.
func NewPromptService(cfg PromptServiceConfig) (*PromptService, error) {
	if cfg.Store == nil {
		return nil, &PromptServiceConfigError{Field: "Store"}
	}
	if cfg.Resolver == nil {
		return nil, &PromptServiceConfigError{Field: "Resolver"}
	}
	return &PromptService{store: cfg.Store, resolver: cfg.Resolver, projects: cfg.Projects}, nil
}

// List returns every prompt of a set as one scope sees it.
func (s *PromptService) List(ctx context.Context, req *ListPromptsRequest) (*ListPromptsResponse, error) {
	if req == nil {
		return nil, &PromptRequestError{Field: "request", Message: "must not be nil"}
	}
	set, known := s.store.Registry().Lookup(prompts.SetKind(req.Set))
	if !known {
		return nil, &prompts.UnknownSetError{Set: prompts.SetKind(req.Set)}
	}
	scope, err := s.scopeRef(ctx, req.TenantID, req.Scope, req.ProjectID)
	if err != nil {
		return nil, err
	}
	records, err := s.store.List(ctx, scope.TenantID, scope.ProjectID, set.Kind())
	if err != nil {
		return nil, err
	}
	held := make(map[string]*prompts.Record, len(records))
	for _, record := range records {
		held[record.Name] = record
	}
	resolved, err := s.resolve(ctx, scope, set.Kind())
	if err != nil {
		return nil, err
	}
	effective := make(map[string]prompts.VersionRef, len(set.Names()))
	for _, ref := range resolved.Refs() {
		effective[ref.Name] = ref
	}

	spec := set.Spec()
	out := &ListPromptsResponse{Set: set.Kind(), Scope: scope.Scope(), Prompts: make([]PromptListEntry, 0, len(spec.Prompts))}
	for _, name := range spec.Names() {
		prompt, _ := spec.Prompt(name)
		entry := PromptListEntry{Name: name, Description: prompt.Description, Effective: effective[name]}
		if record, ok := held[name]; ok {
			entry.Version = record.Version
			entry.FollowParent = record.FollowParent
			entry.Overridden = !record.FollowParent
		}
		out.Prompts = append(out.Prompts, entry)
	}
	return out, nil
}

// Get returns the record a scope holds, or one revision of it. A scope
// whose record follows its parent overrides nothing and answers not-found.
func (s *PromptService) Get(ctx context.Context, req *GetPromptRequest) (*PromptView, error) {
	if req == nil {
		return nil, &PromptRequestError{Field: "request", Message: "must not be nil"}
	}
	if req.Version < 0 {
		return nil, &PromptRequestError{Field: "version", Message: "must not be negative"}
	}
	ref, err := s.ref(ctx, req.PromptAddress)
	if err != nil {
		return nil, err
	}
	record, err := s.store.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	if req.Version == 0 {
		if record.FollowParent {
			return nil, &PromptNotOverriddenError{Ref: ref}
		}
		return recordView(record), nil
	}
	revision, known := record.Revision(req.Version)
	if !known {
		return nil, &prompts.UnknownRevisionError{Ref: ref, Version: req.Version}
	}
	view := recordView(record)
	view.Version = revision.Version
	view.ContentHash = revision.ContentHash
	view.Content = revision.Content
	view.FollowParent = revision.FollowParent
	view.UpdatedAt = revision.CreatedAt
	return view, nil
}

// Effective returns the body a job at the scope runs with, its origin and
// the chain of layers resolution read. It answers even when no layer holds
// a record: the embedded default always does.
func (s *PromptService) Effective(ctx context.Context, addr *PromptAddress) (*EffectivePrompt, error) {
	if addr == nil {
		return nil, &PromptRequestError{Field: "request", Message: "must not be nil"}
	}
	ref, err := s.ref(ctx, *addr)
	if err != nil {
		return nil, err
	}
	content, origin, err := s.effective(ctx, ref)
	if err != nil {
		return nil, err
	}
	chain, err := s.chain(ctx, ref, origin)
	if err != nil {
		return nil, err
	}
	return &EffectivePrompt{
		Set: ref.Set, Name: ref.Name, Scope: ref.Scope(),
		Content: content, Origin: origin, Chain: chain,
	}, nil
}

// Diff compares the body the scope runs with against its parent's.
func (s *PromptService) Diff(ctx context.Context, addr *PromptAddress) (*PromptDiff, error) {
	if addr == nil {
		return nil, &PromptRequestError{Field: "request", Message: "must not be nil"}
	}
	ref, err := s.ref(ctx, *addr)
	if err != nil {
		return nil, err
	}
	content, origin, err := s.effective(ctx, ref)
	if err != nil {
		return nil, err
	}
	parentContent, parentOrigin, err := s.parentEffective(ctx, ref)
	if err != nil {
		return nil, err
	}
	return &PromptDiff{
		Set: ref.Set, Name: ref.Name, Scope: ref.Scope(),
		Origin: origin, ParentOrigin: parentOrigin,
		Same:  content == parentContent,
		Lines: diffLines(parentContent, content),
	}, nil
}

// History returns the identifiers of a scope's record's revisions.
func (s *PromptService) History(ctx context.Context, addr *PromptAddress) (*PromptHistory, error) {
	if addr == nil {
		return nil, &PromptRequestError{Field: "request", Message: "must not be nil"}
	}
	ref, err := s.ref(ctx, *addr)
	if err != nil {
		return nil, err
	}
	revisions, err := s.store.History(ctx, ref)
	if err != nil {
		return nil, err
	}
	out := &PromptHistory{Set: ref.Set, Name: ref.Name, Scope: ref.Scope(), Revisions: make([]PromptRevisionSummary, 0, len(revisions))}
	for _, revision := range revisions {
		out.Revisions = append(out.Revisions, PromptRevisionSummary{
			Version: revision.Version, ContentHash: revision.ContentHash,
			Author: revision.Author, Note: revision.Note,
			FollowParent: revision.FollowParent, CreatedAt: revision.CreatedAt,
		})
	}
	return out, nil
}

// Put writes a new body at a scope, guarded by ExpectedVersion.
func (s *PromptService) Put(ctx context.Context, req *PutPromptRequest) (*PromptView, error) {
	if req == nil {
		return nil, &PromptRequestError{Field: "request", Message: "must not be nil"}
	}
	if strings.TrimSpace(req.Note) == "" {
		return nil, &PromptRequestError{Field: "note", Message: "every edit says why it was made"}
	}
	ref, err := s.writableRef(ctx, req.PromptAddress)
	if err != nil {
		return nil, err
	}
	record, err := s.store.Save(ctx, ref, prompts.Change{
		Stamp:   stamp(ctx, req.Note, req.ExpectedVersion),
		Content: req.Content,
	})
	if err != nil {
		return nil, err
	}
	return recordView(record), nil
}

// Rollback writes an earlier revision's body as a new revision.
func (s *PromptService) Rollback(ctx context.Context, req *RollbackPromptRequest) (*PromptView, error) {
	if req == nil {
		return nil, &PromptRequestError{Field: "request", Message: "must not be nil"}
	}
	if req.ToVersion < 1 {
		return nil, &PromptRequestError{Field: "to_version", Message: "must name a revision"}
	}
	if req.ExpectedVersion < 1 {
		return nil, &PromptRequestError{Field: "expected_version", Message: "must be the version the caller read"}
	}
	ref, err := s.writableRef(ctx, req.PromptAddress)
	if err != nil {
		return nil, err
	}
	record, err := s.store.Rollback(ctx, ref, req.ToVersion, stamp(ctx, req.Note, req.ExpectedVersion))
	if err != nil {
		return nil, err
	}
	return recordView(record), nil
}

// Reset returns a scope's record to following its parent.
func (s *PromptService) Reset(ctx context.Context, req *ResetPromptRequest) (*PromptView, error) {
	if req == nil {
		return nil, &PromptRequestError{Field: "request", Message: "must not be nil"}
	}
	if req.ExpectedVersion < 1 {
		return nil, &PromptRequestError{Field: "expected_version", Message: "must be the version the caller read"}
	}
	ref, err := s.writableRef(ctx, req.PromptAddress)
	if err != nil {
		return nil, err
	}
	record, err := s.store.Reset(ctx, ref, stamp(ctx, req.Note, req.ExpectedVersion))
	if err != nil {
		return nil, err
	}
	return recordView(record), nil
}

// stamp names the caller as the author of a write.
func stamp(ctx context.Context, note string, expected int64) prompts.Stamp {
	return prompts.Stamp{Author: auth.PrincipalFromContext(ctx), Note: note, ExpectedVersion: expected}
}

// writableRef is ref for a write, refusing the system scope.
func (s *PromptService) writableRef(ctx context.Context, addr PromptAddress) (prompts.Ref, error) {
	ref, err := s.ref(ctx, addr)
	if err != nil {
		return prompts.Ref{}, err
	}
	if ref.Scope() == prompts.ScopeSystem {
		return prompts.Ref{}, &PromptRequestError{Field: "scope", Message: "the system scope is read-only; only the boot reconciler writes it"}
	}
	return ref, nil
}

// ref resolves an address to the record it names, refusing an unknown set
// or prompt before the store is touched.
func (s *PromptService) ref(ctx context.Context, addr PromptAddress) (prompts.Ref, error) {
	set, known := s.store.Registry().Lookup(prompts.SetKind(addr.Set))
	if !known {
		return prompts.Ref{}, &prompts.UnknownSetError{Set: prompts.SetKind(addr.Set)}
	}
	if _, known := set.Spec().Prompt(addr.Name); !known {
		return prompts.Ref{}, &prompts.UnknownPromptError{Set: set.Kind(), Prompt: addr.Name}
	}
	ref, err := s.scopeRef(ctx, addr.TenantID, addr.Scope, addr.ProjectID)
	if err != nil {
		return prompts.Ref{}, err
	}
	ref.Set, ref.Name = set.Kind(), addr.Name
	return ref, nil
}

// scopeRef resolves a scope to the tenant and project that own it, proving
// a project scope names a project of the tenant.
func (s *PromptService) scopeRef(ctx context.Context, tenantID, scopeName, projectID string) (prompts.Ref, error) {
	if scopeName == "" {
		scopeName = string(prompts.ScopeTenant)
		if projectID != "" {
			scopeName = string(prompts.ScopeProject)
		}
	}
	scope, known := promptScopes[scopeName]
	if !known {
		return prompts.Ref{}, &PromptRequestError{Field: "scope", Message: "must be tenant, project or system"}
	}
	if scope == prompts.ScopeSystem {
		if projectID != "" {
			return prompts.Ref{}, &PromptRequestError{Field: "project_id", Message: "the system scope has no projects"}
		}
		return prompts.Ref{TenantID: qrdbsdk.SystemTenantID}, nil
	}
	if tenantID == "" {
		return prompts.Ref{}, &PromptRequestError{Field: "tenant_id", Message: "is required"}
	}
	if scope == prompts.ScopeTenant {
		if projectID != "" {
			return prompts.Ref{}, &PromptRequestError{Field: "project_id", Message: "the tenant scope takes no project"}
		}
		return prompts.Ref{TenantID: tenantID}, nil
	}
	if projectID == "" {
		return prompts.Ref{}, &PromptRequestError{Field: "project_id", Message: "is required for the project scope"}
	}
	if s.projects != nil {
		if _, err := s.projects.GetProject(ctx, &GetProjectRequest{TenantID: tenantID, ProjectID: projectID}); err != nil {
			return prompts.Ref{}, err
		}
	}
	return prompts.Ref{TenantID: tenantID, ProjectID: projectID}, nil
}

// resolve resolves set as a job at scope would.
func (s *PromptService) resolve(ctx context.Context, scope prompts.Ref, set prompts.SetKind) (*prompts.ResolvedSet, error) {
	if scope.Scope() == prompts.ScopeSystem {
		return s.resolver.Resolve(ctx, "", "", set)
	}
	return s.resolver.Resolve(ctx, scope.TenantID, scope.ProjectID, set)
}

// effective is the body a job at ref's scope runs ref's prompt with.
func (s *PromptService) effective(ctx context.Context, ref prompts.Ref) (string, prompts.VersionRef, error) {
	resolved, err := s.resolve(ctx, ref, ref.Set)
	if err != nil {
		return "", prompts.VersionRef{}, err
	}
	content, err := resolved.Get(ref.Name)
	if err != nil {
		return "", prompts.VersionRef{}, err
	}
	for _, origin := range resolved.Refs() {
		if origin.Name == ref.Name {
			return content, origin, nil
		}
	}
	return "", prompts.VersionRef{}, &prompts.UnknownPromptError{Set: ref.Set, Prompt: ref.Name}
}

// parentEffective is the body the scope above ref's runs with: the tenant
// for a project, the system for a tenant, the embedded default for the
// system.
func (s *PromptService) parentEffective(ctx context.Context, ref prompts.Ref) (string, prompts.VersionRef, error) {
	switch ref.Scope() {
	case prompts.ScopeProject:
		return s.effective(ctx, prompts.Ref{TenantID: ref.TenantID, Set: ref.Set, Name: ref.Name})
	case prompts.ScopeTenant:
		return s.effective(ctx, prompts.Ref{TenantID: qrdbsdk.SystemTenantID, Set: ref.Set, Name: ref.Name})
	}
	set, _ := s.store.Registry().Lookup(ref.Set)
	spec, _ := set.Spec().Prompt(ref.Name)
	return spec.Default, prompts.VersionRef{Set: ref.Set, Name: ref.Name, Scope: prompts.ScopeEmbedded, Hash: prompts.Hash(spec.Default)}, nil
}

// chain reports every layer resolution reads for ref, nearest first, ending
// with the embedded default.
func (s *PromptService) chain(ctx context.Context, ref prompts.Ref, origin prompts.VersionRef) ([]PromptLayer, error) {
	var layers []prompts.Ref
	if ref.ProjectID != "" {
		layers = append(layers, prompts.Ref{TenantID: ref.TenantID, ProjectID: ref.ProjectID})
	}
	if ref.TenantID != qrdbsdk.SystemTenantID {
		layers = append(layers, prompts.Ref{TenantID: ref.TenantID})
	}
	layers = append(layers, prompts.Ref{TenantID: qrdbsdk.SystemTenantID})

	out := make([]PromptLayer, 0, len(layers)+1)
	for _, layer := range layers {
		layer.Set, layer.Name = ref.Set, ref.Name
		entry := PromptLayer{Scope: layer.Scope(), ScopeID: layer.TenantID, Answered: layer.Scope() == origin.Scope}
		if layer.ProjectID != "" {
			entry.ScopeID = layer.ProjectID
		}
		record, err := s.store.Get(ctx, layer)
		switch {
		case errors.Is(err, prompts.ErrNotFound):
		case err != nil:
			return nil, err
		default:
			entry.Present = true
			entry.Version = record.Version
			entry.Hash = record.ContentHash
			entry.FollowParent = record.FollowParent
		}
		out = append(out, entry)
	}
	set, _ := s.store.Registry().Lookup(ref.Set)
	spec, _ := set.Spec().Prompt(ref.Name)
	return append(out, PromptLayer{
		Scope: prompts.ScopeEmbedded, Present: true, Hash: prompts.Hash(spec.Default),
		Answered: origin.Scope == prompts.ScopeEmbedded,
	}), nil
}

// recordView is the view of a record's current revision.
func recordView(record *prompts.Record) *PromptView {
	ref := record.Ref()
	return &PromptView{
		TenantID: record.TenantID, ProjectID: record.ProjectID, Scope: ref.Scope(),
		Set: record.Set, Name: record.Name,
		Version: record.Version, ContentHash: record.ContentHash, Content: record.Content,
		FollowParent: record.FollowParent, UpdatedAt: record.UpdatedAt,
	}
}
