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

package prompts

import (
	"context"
	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// VersionRef records which body a job ran a prompt with: the scope it came
// from, that scope's record version, and the content hash. It is what makes
// "which words did this job actually use" answerable after the store moved
// on.
type VersionRef struct {
	Set  SetKind `json:"set"`
	Name string  `json:"name"`

	// Scope is the layer that answered, and ScopeID the project or tenant id
	// that owns that layer (empty for embedded).
	Scope   Scope  `json:"scope"`
	ScopeID string `json:"scope_id,omitempty"`

	// Version is the record's version; 0 for the embedded body.
	Version int64  `json:"version"`
	Hash    string `json:"hash"`
}

// Renderer renders one prompt of a set: Render returns the failure, and
// MustRender panics on it, for an assembler with no error to return. Both an embedded *Set and a *ResolvedSet satisfy it, so an assembler is
// written once over either.
type Renderer interface {
	Render(name string, vars Vars) (string, error)
	MustRender(name string, vars Vars) string
}

// ResolvedSet is a set as one job sees it: every prompt's body chosen from
// the inheritance chain, fixed at launch.
//
// IT IS IMMUTABLE. A job renders from the bodies it was launched with even
// when somebody edits the store part way through it, and its refs say which
// those were.
type ResolvedSet struct {
	spec   SetSpec
	bodies map[string]string
	refs   []VersionRef
}

// Kind returns the set's kind.
func (r *ResolvedSet) Kind() SetKind { return r.spec.Kind }

// Get returns one prompt's resolved body.
func (r *ResolvedSet) Get(name string) (string, error) {
	body, known := r.bodies[name]
	if !known {
		return "", &UnknownPromptError{Set: r.spec.Kind, Prompt: name}
	}
	return body, nil
}

// Render renders one prompt from its resolved body.
func (r *ResolvedSet) Render(name string, vars Vars) (string, error) {
	spec, known := r.spec.Prompt(name)
	if !known {
		return "", &UnknownPromptError{Set: r.spec.Kind, Prompt: name}
	}
	return Render(r.spec.Kind, spec, r.bodies[name], vars)
}

// MustRender renders one prompt or panics. Every resolved body was validated
// against the spec at resolution, so — as for Set.MustRender — only an
// assembler naming an undeclared variable or withholding a required one can
// fail here, a defect in this binary.
func (r *ResolvedSet) MustRender(name string, vars Vars) string {
	rendered, err := r.Render(name, vars)
	if err != nil {
		panic(err)
	}
	return rendered
}

// Refs returns the version every prompt resolved to, ordered by name. A nil
// set — a job whose runner renders no prompts — has none.
func (r *ResolvedSet) Refs() []VersionRef {
	if r == nil {
		return nil
	}
	return append([]VersionRef(nil), r.refs...)
}

// RendererOr is what a caller renders embedded's prompts from: resolved when
// it is a resolution of that same set, otherwise embedded itself. It serves
// the assemblers that also run outside a launched job — a test, a run built
// directly — where there is no resolved set and the build's defaults are the
// only answer.
func RendererOr(resolved *ResolvedSet, embedded *Set) Renderer {
	if resolved != nil && resolved.Kind() == embedded.Kind() {
		return resolved
	}
	return embedded
}

// Resolver chooses, for each prompt of a set, the body a job runs with:
// project, then tenant, then system, then the embedded default. A record
// that follows its parent is skipped, so an unedited copy tracks the scope
// above it without being rewritten on every build.
type Resolver struct {
	store    *Store
	registry *Registry
}

// NewResolver builds a resolver over store. A nil store resolves every
// prompt to its embedded default, which is what a node with no qrdb has.
func NewResolver(store *Store) *Resolver {
	resolver := &Resolver{store: store, registry: registry}
	if store != nil {
		resolver.registry = store.registry
	}
	return resolver
}

// Resolve resolves set kind for one job of tenantID's projectID. An empty
// projectID skips the project layer; an empty tenantID skips the tenant's
// layers and resolves from the system set.
func (r *Resolver) Resolve(ctx context.Context, tenantID, projectID string, kind SetKind) (*ResolvedSet, error) {
	set, known := r.registry.Lookup(kind)
	if !known {
		return nil, &ResolveError{TenantID: tenantID, ProjectID: projectID, Set: kind, Cause: &UnknownSetError{Set: kind}}
	}
	spec := set.Spec()
	resolved := &ResolvedSet{spec: spec, bodies: make(map[string]string, len(spec.Prompts))}
	layers := chain(tenantID, projectID)
	for _, name := range spec.Names() {
		prompt, _ := spec.Prompt(name)
		body, ref, err := r.resolveOne(ctx, kind, prompt, layers)
		if err != nil {
			return nil, &ResolveError{TenantID: tenantID, ProjectID: projectID, Set: kind, Prompt: name, Cause: err}
		}
		resolved.bodies[name] = body
		resolved.refs = append(resolved.refs, ref)
	}
	return resolved, nil
}

// chain lists the scopes consulted for one job, nearest first.
func chain(tenantID, projectID string) []Ref {
	var layers []Ref
	if tenantID != "" && projectID != "" {
		layers = append(layers, Ref{TenantID: tenantID, ProjectID: projectID})
	}
	if tenantID != "" && tenantID != qrdbsdk.SystemTenantID {
		layers = append(layers, Ref{TenantID: tenantID})
	}
	// The system tenant's tenant-wide set is its own tenant layer and every
	// other tenant's last stored layer.
	return append(layers, Ref{TenantID: qrdbsdk.SystemTenantID})
}

// resolveOne walks the chain for one prompt.
func (r *Resolver) resolveOne(ctx context.Context, kind SetKind, prompt PromptSpec, layers []Ref) (string, VersionRef, error) {
	if r.store != nil {
		for _, layer := range layers {
			layer.Set, layer.Name = kind, prompt.Name
			record, err := r.store.Get(ctx, layer)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return "", VersionRef{}, err
			}
			if record.FollowParent {
				continue
			}
			// The specification may have changed since the record was saved; a
			// body the engine can no longer parse refuses the launch by name.
			if err := Validate(kind, prompt, record.Content); err != nil {
				return "", VersionRef{}, err
			}
			return record.Content, VersionRef{
				Set: kind, Name: prompt.Name, Scope: layer.Scope(), ScopeID: scopeID(layer),
				Version: record.Version, Hash: record.ContentHash,
			}, nil
		}
	}
	return prompt.Default, VersionRef{
		Set: kind, Name: prompt.Name, Scope: ScopeEmbedded, Hash: Hash(prompt.Default),
	}, nil
}

// scopeID names the owner of a layer: its project, else its tenant.
func scopeID(layer Ref) string {
	if layer.ProjectID != "" {
		return layer.ProjectID
	}
	return layer.TenantID
}
