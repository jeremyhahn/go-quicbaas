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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// WorkflowLoader loads, validates, and manages workflow definitions from
// YAML files or programmatic registration. It uses a sync.Map for lock-free
// concurrent access to the workflow registry.
type WorkflowLoader struct {
	workflows sync.Map // map[string]*WorkflowDefinition
}

// NewWorkflowLoader creates a new WorkflowLoader.
func NewWorkflowLoader() *WorkflowLoader {
	return &WorkflowLoader{}
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
// The definition is validated before registration. Returns an error
// if validation fails or a workflow with the same name already exists.
func (wl *WorkflowLoader) Register(def *WorkflowDefinition) error {
	if err := def.Validate(); err != nil {
		return err
	}
	if _, loaded := wl.workflows.LoadOrStore(def.Name, def); loaded {
		return &WorkflowAlreadyExistsError{Name: def.Name}
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
