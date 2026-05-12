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
type WorkflowPersistor interface {
	// Save writes the definition. Overwrites any existing entry with the same
	// name. Errors should be returned to the caller; the loader rolls back
	// in-memory state on failure to keep memory consistent with storage.
	Save(ctx context.Context, def *WorkflowDefinition) error

	// Remove deletes the entry by name. Returns nil if the name was not
	// present (idempotent).
	Remove(ctx context.Context, name string) error

	// LoadAll returns every persisted workflow. Called once at startup to
	// rehydrate the in-memory cache.
	LoadAll(ctx context.Context) ([]*WorkflowDefinition, error)
}

// WorkflowLoader loads, validates, and manages workflow definitions from
// YAML files or programmatic registration. It uses a sync.Map for lock-free
// concurrent access to the workflow registry.
//
// When a WorkflowPersistor is attached, every Register/Update writes through
// the persistor before updating the in-memory cache, and Delete removes the
// persistor entry first. The orchestrator's startup hook calls Rehydrate to
// load any previously stored definitions back into the cache.
type WorkflowLoader struct {
	workflows sync.Map // map[string]*WorkflowDefinition
	persistor WorkflowPersistor
}

// NewWorkflowLoader creates a new WorkflowLoader without a persistor. Useful
// for tests and ephemeral processes where workflow durability does not matter.
func NewWorkflowLoader() *WorkflowLoader {
	return &WorkflowLoader{}
}

// SetPersistor attaches a backing store. Subsequent Register/Update/Delete
// calls flow through the persistor; existing in-memory entries are NOT pushed
// to the persistor automatically. Pair with Rehydrate at startup.
func (wl *WorkflowLoader) SetPersistor(p WorkflowPersistor) {
	wl.persistor = p
}

// Rehydrate clears the in-memory cache and reloads it from the persistor. Call
// once at startup, after SetPersistor, to restore any previously saved
// workflow definitions into memory. Returns the count of workflows loaded.
func (wl *WorkflowLoader) Rehydrate(ctx context.Context) (int, error) {
	if wl.persistor == nil {
		return 0, nil
	}
	defs, err := wl.persistor.LoadAll(ctx)
	if err != nil {
		return 0, err
	}
	// Reset the cache so a Rehydrate after a partial state has clean view.
	wl.workflows.Range(func(k, _ any) bool {
		wl.workflows.Delete(k)
		return true
	})
	for _, def := range defs {
		if def == nil || def.Name == "" {
			continue
		}
		wl.workflows.Store(def.Name, def)
	}
	return len(defs), nil
}

// LoadFromFile loads a workflow definition from a YAML file at the given path.
// The loaded workflow is validated and stored in the internal registry.
func (wl *WorkflowLoader) LoadFromFile(path string) (*WorkflowDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &WorkflowLoadError{Path: path, Cause: err}
	}

	var def WorkflowDefinition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, &WorkflowParseError{Path: path, Cause: err}
	}

	if err := def.Validate(); err != nil {
		return nil, err
	}

	wl.workflows.Store(def.Name, &def)
	return &def, nil
}

// LoadFromDir loads all YAML files (*.yaml, *.yml) from the given directory.
// Each file is loaded and validated individually. Returns the first error
// encountered, if any.
func (wl *WorkflowLoader) LoadFromDir(dir string) error {
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
		if _, err := wl.LoadFromFile(fullPath); err != nil {
			return err
		}
	}
	return nil
}

// Register adds a workflow definition directly to the registry.
// The definition is validated before registration. When a persistor is
// attached, the definition is written through to the persistor first; on
// disk-write failure the in-memory store is rolled back so memory and
// storage never diverge. Returns an error if validation fails or a workflow
// with the same name already exists.
func (wl *WorkflowLoader) Register(def *WorkflowDefinition) error {
	if err := def.Validate(); err != nil {
		return err
	}
	if _, loaded := wl.workflows.LoadOrStore(def.Name, def); loaded {
		return &WorkflowAlreadyExistsError{Name: def.Name}
	}
	if wl.persistor != nil {
		if err := wl.persistor.Save(context.Background(), def); err != nil {
			wl.workflows.Delete(def.Name)
			return err
		}
	}
	return nil
}

// Update replaces an existing workflow definition in the registry.
// The definition is validated before updating. Returns WorkflowNotFoundError
// if no workflow with the given name exists. When a persistor is attached
// the new definition is persisted; on persistor failure the previous
// in-memory definition is restored.
func (wl *WorkflowLoader) Update(def *WorkflowDefinition) error {
	if err := def.Validate(); err != nil {
		return err
	}
	prevAny, ok := wl.workflows.Load(def.Name)
	if !ok {
		return &WorkflowNotFoundError{Name: def.Name}
	}
	wl.workflows.Store(def.Name, def)
	if wl.persistor != nil {
		if err := wl.persistor.Save(context.Background(), def); err != nil {
			if prev, ok := prevAny.(*WorkflowDefinition); ok {
				wl.workflows.Store(def.Name, prev)
			}
			return err
		}
	}
	return nil
}

// Delete removes a workflow definition from the registry by name.
// Returns WorkflowNotFoundError if no workflow with the given name exists.
// When a persistor is attached the persistor entry is removed; on persistor
// failure the in-memory entry is restored.
func (wl *WorkflowLoader) Delete(name string) error {
	prevAny, ok := wl.workflows.Load(name)
	if !ok {
		return &WorkflowNotFoundError{Name: name}
	}
	wl.workflows.Delete(name)
	if wl.persistor != nil {
		if err := wl.persistor.Remove(context.Background(), name); err != nil {
			if prev, ok := prevAny.(*WorkflowDefinition); ok {
				wl.workflows.Store(name, prev)
			}
			return err
		}
	}
	return nil
}

// Get returns a workflow definition by name.
func (wl *WorkflowLoader) Get(name string) (*WorkflowDefinition, error) {
	val, ok := wl.workflows.Load(name)
	if !ok {
		return nil, &WorkflowNotFoundError{Name: name}
	}
	def, ok := val.(*WorkflowDefinition)
	if !ok {
		return nil, &WorkflowNotFoundError{Name: name}
	}
	return def, nil
}

// List returns the names of all loaded workflow definitions, sorted alphabetically.
func (wl *WorkflowLoader) List() []string {
	var names []string
	wl.workflows.Range(func(key, _ any) bool {
		k, ok := key.(string)
		if !ok {
			return true
		}
		names = append(names, k)
		return true
	})
	sort.Strings(names)
	return names
}

// ListDefinitions returns all loaded workflow definitions, sorted by name.
func (wl *WorkflowLoader) ListDefinitions() []*WorkflowDefinition {
	var defs []*WorkflowDefinition
	wl.workflows.Range(func(_, value any) bool {
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
