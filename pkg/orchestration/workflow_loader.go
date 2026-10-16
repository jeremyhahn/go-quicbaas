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

package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// WorkflowPersistor is the storage backend the loader writes to on every
// Register/Update/Delete. It must be safe for concurrent use. Implementations
// typically wrap the QuicBaaS internal KV store so workflows survive daemon
// restarts and replicate across raft peers — exactly like every other
// orchestration entity. An in-memory loader without a persistor is acceptable
// only for tests and ephemeral test daemons.
//
// Every entry is tenant-scoped: a definition is filed under its TenantID and
// two tenants may hold the same workflow name without colliding.
type WorkflowPersistor interface {
	// Save writes the definition under (def.TenantID, def.Name). Overwrites
	// any existing entry with the same tenant and name. Errors should be
	// returned to the caller; the loader rolls back in-memory state on
	// failure to keep memory consistent with storage.
	Save(ctx context.Context, def *WorkflowDefinition) error

	// Remove deletes the tenant's entry by name. Returns nil if the name was
	// not present (idempotent).
	Remove(ctx context.Context, tenantID, name string) error

	// Load returns every persisted workflow owned by tenantID.
	Load(ctx context.Context, tenantID string) ([]*WorkflowDefinition, error)

	// LoadAll returns every persisted workflow across all tenants, each
	// carrying its TenantID. Called once at startup to rehydrate the
	// in-memory cache.
	LoadAll(ctx context.Context) ([]*WorkflowDefinition, error)
}

// WorkflowLoader loads, validates, and manages workflow definitions from
// YAML files or programmatic registration. It uses a sync.Map for lock-free
// concurrent access to the workflow registry.
//
// Every verb takes the tenant the workflow belongs to. Definitions are keyed
// by (tenant, name), so a name is unique within a tenant and two tenants
// never observe each other's workflows.
//
// When a WorkflowPersistor is attached, every Register/Update writes through
// the persistor before updating the in-memory cache, and Delete removes the
// persistor entry first. The orchestrator's startup hook calls RehydrateAll
// to load any previously stored definitions back into the cache.
type WorkflowLoader struct {
	workflows sync.Map // map[workflowKey]*WorkflowDefinition
	persistor WorkflowPersistor
}

// workflowKey addresses one definition in the in-memory registry.
type workflowKey struct {
	tenantID string
	name     string
}

// NewWorkflowLoader creates a new WorkflowLoader without a persistor. Useful
// for tests and ephemeral processes where workflow durability does not matter.
func NewWorkflowLoader() *WorkflowLoader {
	return &WorkflowLoader{}
}

// SetPersistor attaches a backing store. Subsequent Register/Update/Delete
// calls flow through the persistor; existing in-memory entries are NOT pushed
// to the persistor automatically. Pair with RehydrateAll at startup.
func (wl *WorkflowLoader) SetPersistor(p WorkflowPersistor) {
	wl.persistor = p
}

// requireTenant refuses an empty tenant. Every record key begins with its
// owning tenant, so an empty segment would collapse distinct tenants'
// keyspaces.
func requireTenant(tenantID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return &WorkflowValidationError{Field: "tenant_id", Message: "workflow tenant is required"}
	}
	return nil
}

// store files a definition under its tenant and name.
func (wl *WorkflowLoader) store(tenantID string, def *WorkflowDefinition) {
	def.TenantID = tenantID
	wl.workflows.Store(workflowKey{tenantID: tenantID, name: def.Name}, def)
}

// clearTenant drops every cached entry owned by tenantID. An empty tenant
// clears everything.
func (wl *WorkflowLoader) clearTenant(tenantID string) {
	wl.workflows.Range(func(k, _ any) bool {
		key, ok := k.(workflowKey)
		if !ok || tenantID == "" || key.tenantID == tenantID {
			wl.workflows.Delete(k)
		}
		return true
	})
}

// fill files every loaded definition under its own TenantID, skipping
// entries that cannot be addressed.
func (wl *WorkflowLoader) fill(defs []*WorkflowDefinition) {
	for _, def := range defs {
		if def == nil || def.Name == "" || def.TenantID == "" {
			continue
		}
		wl.store(def.TenantID, def)
	}
}

// Rehydrate replaces the tenant's cached definitions with those held by the
// persistor. Returns the count of workflows loaded for that tenant.
func (wl *WorkflowLoader) Rehydrate(ctx context.Context, tenantID string) (int, error) {
	if err := requireTenant(tenantID); err != nil {
		return 0, err
	}
	if wl.persistor == nil {
		return 0, nil
	}
	defs, err := wl.persistor.Load(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	wl.clearTenant(tenantID)
	for _, def := range defs {
		if def == nil || def.Name == "" {
			continue
		}
		wl.store(tenantID, def)
	}
	return len(defs), nil
}

// RehydrateAll clears the in-memory cache and reloads every tenant's
// definitions from the persistor. Call once at startup, after SetPersistor.
// Returns the count of workflows loaded.
func (wl *WorkflowLoader) RehydrateAll(ctx context.Context) (int, error) {
	if wl.persistor == nil {
		return 0, nil
	}
	defs, err := wl.persistor.LoadAll(ctx)
	if err != nil {
		return 0, err
	}
	// Reset the cache so a rehydrate after a partial state has a clean view.
	wl.clearTenant("")
	wl.fill(defs)
	return len(defs), nil
}

// LoadFromFile loads a workflow definition from a YAML file at the given path
// into the tenant's registry. The loaded workflow is validated and stored.
func (wl *WorkflowLoader) LoadFromFile(tenantID, path string) (*WorkflowDefinition, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &WorkflowLoadError{Path: path, Cause: err}
	}

	var def WorkflowDefinition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, &WorkflowParseError{Path: path, Cause: err}
	}
	if err := RefuseRetiredWorkflowFields(path, data); err != nil {
		return nil, err
	}

	if err := def.Validate(); err != nil {
		return nil, err
	}

	wl.store(tenantID, &def)
	return &def, nil
}

// LoadFromDir loads all YAML files (*.yaml, *.yml) from the given directory
// into the tenant's registry. Each file is loaded and validated individually.
// Returns the first error encountered, if any.
func (wl *WorkflowLoader) LoadFromDir(tenantID, dir string) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return &WorkflowLoadError{Path: dir, Cause: err}
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		fullPath := filepath.Join(dir, name)
		if _, err := wl.LoadFromFile(tenantID, fullPath); err != nil {
			return err
		}
	}
	return nil
}

// Register adds a workflow definition to the tenant's registry.
// The definition is validated before registration. When a persistor is
// attached, the definition is written through to the persistor first; on
// write failure the in-memory store is rolled back so memory and storage
// never diverge. Returns an error if validation fails or a workflow with the
// same name already exists in the tenant.
func (wl *WorkflowLoader) Register(tenantID string, def *WorkflowDefinition) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	if err := def.Validate(); err != nil {
		return err
	}
	def.TenantID = tenantID
	key := workflowKey{tenantID: tenantID, name: def.Name}
	if _, loaded := wl.workflows.LoadOrStore(key, def); loaded {
		return &WorkflowAlreadyExistsError{TenantID: tenantID, Name: def.Name}
	}
	if wl.persistor != nil {
		if err := wl.persistor.Save(context.Background(), def); err != nil {
			wl.workflows.Delete(key)
			return err
		}
	}
	return nil
}

// Update replaces an existing workflow definition in the tenant's registry.
// The definition is validated before updating. Returns WorkflowNotFoundError
// if no workflow with the given name exists in the tenant. When a persistor
// is attached the new definition is persisted; on persistor failure the
// previous in-memory definition is restored.
func (wl *WorkflowLoader) Update(tenantID string, def *WorkflowDefinition) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	if err := def.Validate(); err != nil {
		return err
	}
	key := workflowKey{tenantID: tenantID, name: def.Name}
	prevAny, ok := wl.workflows.Load(key)
	if !ok {
		return &WorkflowNotFoundError{TenantID: tenantID, Name: def.Name}
	}
	wl.store(tenantID, def)
	if wl.persistor != nil {
		if err := wl.persistor.Save(context.Background(), def); err != nil {
			if prev, ok := prevAny.(*WorkflowDefinition); ok {
				wl.workflows.Store(key, prev)
			}
			return err
		}
	}
	return nil
}

// Delete removes a workflow definition from the tenant's registry by name.
// Returns WorkflowNotFoundError if no workflow with the given name exists in
// the tenant. When a persistor is attached the persistor entry is removed;
// on persistor failure the in-memory entry is restored.
func (wl *WorkflowLoader) Delete(tenantID, name string) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	key := workflowKey{tenantID: tenantID, name: name}
	prevAny, ok := wl.workflows.Load(key)
	if !ok {
		return &WorkflowNotFoundError{TenantID: tenantID, Name: name}
	}
	wl.workflows.Delete(key)
	if wl.persistor != nil {
		if err := wl.persistor.Remove(context.Background(), tenantID, name); err != nil {
			if prev, ok := prevAny.(*WorkflowDefinition); ok {
				wl.workflows.Store(key, prev)
			}
			return err
		}
	}
	return nil
}

// Get returns the tenant's workflow definition by name.
func (wl *WorkflowLoader) Get(tenantID, name string) (*WorkflowDefinition, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	val, ok := wl.workflows.Load(workflowKey{tenantID: tenantID, name: name})
	if !ok {
		return nil, &WorkflowNotFoundError{TenantID: tenantID, Name: name}
	}
	def, ok := val.(*WorkflowDefinition)
	if !ok {
		return nil, &WorkflowNotFoundError{TenantID: tenantID, Name: name}
	}
	return def, nil
}

// List returns the names of the tenant's loaded workflow definitions, sorted
// alphabetically.
func (wl *WorkflowLoader) List(tenantID string) []string {
	defs := wl.ListDefinitions(tenantID)
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Name)
	}
	return names
}

// ListDefinitions returns the tenant's loaded workflow definitions, sorted by
// name.
func (wl *WorkflowLoader) ListDefinitions(tenantID string) []*WorkflowDefinition {
	var defs []*WorkflowDefinition
	wl.workflows.Range(func(k, value any) bool {
		key, ok := k.(workflowKey)
		if !ok || key.tenantID != tenantID {
			return true
		}
		def, ok := value.(*WorkflowDefinition)
		if !ok {
			return true
		}
		defs = append(defs, def)
		return true
	})
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Name < defs[j].Name
	})
	return defs
}
