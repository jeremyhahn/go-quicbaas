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
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// =============================================================================
// ProjectStore — Interface
// =============================================================================

// ProjectStore defines the persistence contract for Project entities.
type ProjectStore interface {
	// Create persists a new project. Returns ProjectAlreadyExistsError if
	// a project with the same TenantID+Name already exists.
	Create(ctx context.Context, project *Project) error

	// Get retrieves a project by its ID within the given tenant.
	Get(ctx context.Context, tenantID, projectID string) (*Project, error)

	// GetByName retrieves a project by its name within the given tenant.
	GetByName(ctx context.Context, tenantID, name string) (*Project, error)

	// Update replaces the stored project. Returns ProjectNotFoundError if not found.
	Update(ctx context.Context, project *Project) error

	// Delete removes a project by ID. Returns ProjectNotFoundError if not found.
	Delete(ctx context.Context, tenantID, projectID string) error

	// ListByTenant returns all projects owned by the given tenant.
	ListByTenant(ctx context.Context, tenantID string) ([]*Project, error)
}

// =============================================================================
// projectEntry — CAS-friendly storage cell
// =============================================================================

// projectEntry wraps serialized project data in a pointer-comparable struct
// so that sync.Map.CompareAndSwap works correctly (pointers compare by identity).
type projectEntry struct {
	data []byte
}

// =============================================================================
// InMemoryProjectStore — lock-free in-process store
// =============================================================================

// inMemoryProjectStore implements ProjectStore using sync.Map for lock-free
// concurrent access. A secondary name index (tenantID+"/"+name → projectID)
// enables O(1) name lookups.
type inMemoryProjectStore struct {
	projects  sync.Map // map[string]*projectEntry  key: "tenantID/projectID"
	nameIndex sync.Map // map[string]string          key: "tenantID/name" → projectID
	count     atomic.Int64
}

// NewInMemoryProjectStore creates a new in-memory project store.
func NewInMemoryProjectStore() ProjectStore {
	return &inMemoryProjectStore{}
}

// projectKey builds the primary composite key.
func projectKey(tenantID, projectID string) string {
	return tenantID + "/" + projectID
}

// projectNameKey builds the name-index key.
func projectNameKey(tenantID, name string) string {
	return tenantID + "/name/" + name
}

// Create persists a new project.
func (s *inMemoryProjectStore) Create(ctx context.Context, project *Project) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if project == nil {
		return &ProjectInvalidError{Message: "project must not be nil"}
	}
	if project.ID == "" {
		return &ProjectInvalidError{Field: "id", Message: "project ID must not be empty"}
	}
	if project.TenantID == "" {
		return &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID must not be empty"}
	}
	if project.Name == "" {
		return &ProjectInvalidError{Field: "name", Message: "project name must not be empty"}
	}

	now := time.Now().UTC()
	if project.CreatedAt.IsZero() {
		project.CreatedAt = now
	}
	project.UpdatedAt = now

	data, err := json.Marshal(project)
	if err != nil {
		return &ProjectStoreError{Message: "failed to marshal project: " + err.Error()}
	}

	// Reserve the name slot first to catch duplicates atomically.
	nameKey := projectNameKey(project.TenantID, project.Name)
	if _, loaded := s.nameIndex.LoadOrStore(nameKey, project.ID); loaded {
		return &ProjectAlreadyExistsError{TenantID: project.TenantID, Name: project.Name}
	}

	pk := projectKey(project.TenantID, project.ID)
	if _, loaded := s.projects.LoadOrStore(pk, &projectEntry{data: data}); loaded {
		// ID collision — roll back the name reservation.
		s.nameIndex.Delete(nameKey)
		return &ProjectAlreadyExistsError{TenantID: project.TenantID, Name: project.Name}
	}

	s.count.Add(1)
	return nil
}

// Get retrieves a project by its ID within the given tenant.
func (s *inMemoryProjectStore) Get(ctx context.Context, tenantID, projectID string) (*Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID must not be empty"}
	}
	if projectID == "" {
		return nil, &ProjectInvalidError{Field: "id", Message: "project ID must not be empty"}
	}

	val, ok := s.projects.Load(projectKey(tenantID, projectID))
	if !ok {
		return nil, &ProjectStoreNotFoundError{TenantID: tenantID, ProjectID: projectID}
	}

	return s.unmarshalEntry(val)
}

// GetByName retrieves a project by its name within the given tenant.
func (s *inMemoryProjectStore) GetByName(ctx context.Context, tenantID, name string) (*Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID must not be empty"}
	}
	if name == "" {
		return nil, &ProjectInvalidError{Field: "name", Message: "project name must not be empty"}
	}

	rawID, ok := s.nameIndex.Load(projectNameKey(tenantID, name))
	if !ok {
		return nil, &ProjectStoreNotFoundError{TenantID: tenantID, Name: name}
	}
	projectID, ok := rawID.(string)
	if !ok {
		return nil, &ProjectStoreError{Message: "name index value is not a string"}
	}

	return s.Get(ctx, tenantID, projectID)
}

// Update replaces the stored project.
func (s *inMemoryProjectStore) Update(ctx context.Context, project *Project) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if project == nil {
		return &ProjectInvalidError{Message: "project must not be nil"}
	}
	if project.ID == "" {
		return &ProjectInvalidError{Field: "id", Message: "project ID must not be empty"}
	}
	if project.TenantID == "" {
		return &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID must not be empty"}
	}

	pk := projectKey(project.TenantID, project.ID)

	// CAS loop for lock-free update.
	for {
		existing, ok := s.projects.Load(pk)
		if !ok {
			return &ProjectStoreNotFoundError{TenantID: project.TenantID, ProjectID: project.ID}
		}

		oldEntry, ok := existing.(*projectEntry)
		if !ok {
			return &ProjectStoreError{Message: "project store value is not *projectEntry"}
		}

		// If name changed, update the index.
		var current Project
		if err := json.Unmarshal(oldEntry.data, &current); err != nil {
			return &ProjectStoreError{Message: "failed to unmarshal existing project: " + err.Error()}
		}

		if project.Name != "" && project.Name != current.Name {
			// Remove old name slot, reserve new one.
			oldNameKey := projectNameKey(project.TenantID, current.Name)
			newNameKey := projectNameKey(project.TenantID, project.Name)
			if _, loaded := s.nameIndex.LoadOrStore(newNameKey, project.ID); loaded {
				return &ProjectAlreadyExistsError{TenantID: project.TenantID, Name: project.Name}
			}
			s.nameIndex.Delete(oldNameKey)
		}

		project.UpdatedAt = time.Now().UTC()
		if project.CreatedAt.IsZero() {
			project.CreatedAt = current.CreatedAt
		}

		newData, err := json.Marshal(project)
		if err != nil {
			return &ProjectStoreError{Message: "failed to marshal updated project: " + err.Error()}
		}

		if s.projects.CompareAndSwap(pk, oldEntry, &projectEntry{data: newData}) {
			return nil
		}
		// Another goroutine changed it; retry.
	}
}

// Delete removes a project and its name-index entry.
func (s *inMemoryProjectStore) Delete(ctx context.Context, tenantID, projectID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenantID == "" {
		return &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID must not be empty"}
	}
	if projectID == "" {
		return &ProjectInvalidError{Field: "id", Message: "project ID must not be empty"}
	}

	pk := projectKey(tenantID, projectID)

	existing, loaded := s.projects.LoadAndDelete(pk)
	if !loaded {
		return &ProjectStoreNotFoundError{TenantID: tenantID, ProjectID: projectID}
	}

	// Clean up name index.
	entry, ok := existing.(*projectEntry)
	if ok {
		var project Project
		if err := json.Unmarshal(entry.data, &project); err == nil {
			s.nameIndex.Delete(projectNameKey(tenantID, project.Name))
		}
	}

	s.count.Add(-1)
	return nil
}

// ListByTenant returns all projects owned by the given tenant.
func (s *inMemoryProjectStore) ListByTenant(ctx context.Context, tenantID string) ([]*Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tenantID == "" {
		return nil, &ProjectInvalidError{Field: "tenant_id", Message: "tenant ID must not be empty"}
	}

	prefix := tenantID + "/"
	var projects []*Project
	var unmarshalErr error

	s.projects.Range(func(key, value interface{}) bool {
		if ctx.Err() != nil {
			return false
		}
		keyStr, ok := key.(string)
		if !ok || !strings.HasPrefix(keyStr, prefix) {
			return true
		}
		// Skip name index entries stored in a separate sync.Map.
		project, err := s.unmarshalEntry(value)
		if err != nil {
			unmarshalErr = err
			return false
		}
		projects = append(projects, project)
		return true
	})

	if unmarshalErr != nil {
		return nil, unmarshalErr
	}

	return projects, nil
}

// unmarshalEntry decodes a sync.Map value into a Project.
func (s *inMemoryProjectStore) unmarshalEntry(val interface{}) (*Project, error) {
	entry, ok := val.(*projectEntry)
	if !ok {
		return nil, &ProjectStoreError{Message: "store value is not *projectEntry"}
	}
	var project Project
	if err := json.Unmarshal(entry.data, &project); err != nil {
		return nil, &ProjectStoreError{Message: "failed to unmarshal project: " + err.Error()}
	}
	return &project, nil
}

// Compile-time interface satisfaction check.
var _ ProjectStore = (*inMemoryProjectStore)(nil)
