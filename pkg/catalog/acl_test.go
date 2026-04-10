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

package catalog

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// aclTestCatalogManager implements CatalogManager for ACL testing.
type aclTestCatalogManager struct {
	mu      sync.RWMutex
	entries map[string]*CatalogEntry
	closed  bool
}

func newACLTestCatalogManager() *aclTestCatalogManager {
	return &aclTestCatalogManager{
		entries: make(map[string]*CatalogEntry),
	}
}

func (m *aclTestCatalogManager) Put(ctx context.Context, entry *CatalogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return NewCatalogClosedError()
	}
	m.entries[entry.Key] = entry.Clone()
	return nil
}

func (m *aclTestCatalogManager) Get(ctx context.Context, key string) (*CatalogEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return nil, NewCatalogClosedError()
	}
	entry, ok := m.entries[key]
	if !ok {
		return nil, NewEntryNotFoundError(key)
	}
	return entry.Clone(), nil
}

func (m *aclTestCatalogManager) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return NewCatalogClosedError()
	}
	delete(m.entries, key)
	return nil
}

func (m *aclTestCatalogManager) Update(ctx context.Context, key string, updater func(*CatalogEntry) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return NewCatalogClosedError()
	}
	entry, ok := m.entries[key]
	if !ok {
		return NewEntryNotFoundError(key)
	}
	if err := updater(entry); err != nil {
		return err
	}
	entry.ModifiedAt = time.Now().UnixNano()
	entry.Version++
	return nil
}

func (m *aclTestCatalogManager) Exists(ctx context.Context, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return false, NewCatalogClosedError()
	}
	_, ok := m.entries[key]
	return ok, nil
}

func (m *aclTestCatalogManager) AddLocation(ctx context.Context, key string, loc *StoreLocation) error {
	return nil
}

func (m *aclTestCatalogManager) RemoveLocation(ctx context.Context, key string, store string) error {
	return nil
}

func (m *aclTestCatalogManager) SetPrimaryStore(ctx context.Context, key string, store string) error {
	return nil
}

func (m *aclTestCatalogManager) GetLocations(ctx context.Context, key string) ([]*StoreLocation, error) {
	return nil, nil
}

func (m *aclTestCatalogManager) UpdateLocationStatus(ctx context.Context, key, store string, status LocationStatus) error {
	return nil
}

func (m *aclTestCatalogManager) ListByStore(ctx context.Context, store string, opts *ListOptions) (*ListResult, error) {
	return &ListResult{}, nil
}

func (m *aclTestCatalogManager) ListByTenant(ctx context.Context, tenant string, opts *ListOptions) (*ListResult, error) {
	return &ListResult{}, nil
}

func (m *aclTestCatalogManager) ListByPrefix(ctx context.Context, prefix string, opts *ListOptions) (*ListResult, error) {
	return &ListResult{}, nil
}

func (m *aclTestCatalogManager) ListByStatus(ctx context.Context, status EntryStatus, opts *ListOptions) (*ListResult, error) {
	return &ListResult{}, nil
}

func (m *aclTestCatalogManager) ListByTag(ctx context.Context, tag, value string, opts *ListOptions) (*ListResult, error) {
	return &ListResult{}, nil
}

func (m *aclTestCatalogManager) ListByEncryptionKey(ctx context.Context, keyID string, opts *ListOptions) (*ListResult, error) {
	return &ListResult{}, nil
}

func (m *aclTestCatalogManager) Search(ctx context.Context, query *SearchQuery) (*SearchResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return nil, NewCatalogClosedError()
	}
	result := &SearchResult{
		Entries: make([]*CatalogEntry, 0),
	}
	for _, entry := range m.entries {
		result.Entries = append(result.Entries, entry.Clone())
	}
	return result, nil
}

func (m *aclTestCatalogManager) EnqueueReplication(ctx context.Context, key string, targets []string, priority int) error {
	return nil
}

func (m *aclTestCatalogManager) EnqueueArchive(ctx context.Context, key string, targetStore string) error {
	return nil
}

func (m *aclTestCatalogManager) EnqueueVerify(ctx context.Context, key string, stores []string) error {
	return nil
}

func (m *aclTestCatalogManager) DequeueReplication(ctx context.Context, limit int) ([]*ReplicationTask, error) {
	return nil, nil
}

func (m *aclTestCatalogManager) DequeueArchive(ctx context.Context, limit int) ([]*ArchiveTask, error) {
	return nil, nil
}

func (m *aclTestCatalogManager) DequeueVerify(ctx context.Context, limit int) ([]*VerifyTask, error) {
	return nil, nil
}

func (m *aclTestCatalogManager) GetTenantUsage(ctx context.Context, tenant string) (*TenantUsage, error) {
	return nil, nil
}

func (m *aclTestCatalogManager) GetStoreUsage(ctx context.Context, store string) (*StoreUsage, error) {
	return nil, nil
}

func (m *aclTestCatalogManager) UpdateAccessTime(ctx context.Context, key string) error {
	return nil
}

func (m *aclTestCatalogManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *aclTestCatalogManager) Sync() error {
	return nil
}

// aclTestLogger implements interfaces.Logger for ACL testing.
type aclTestLogger struct{}

func (l *aclTestLogger) Debug(msg string)                          {}
func (l *aclTestLogger) Debugf(format string, args ...interface{}) {}
func (l *aclTestLogger) Info(msg string)                           {}
func (l *aclTestLogger) Infof(format string, args ...interface{})  {}
func (l *aclTestLogger) Warn(msg string)                           {}
func (l *aclTestLogger) Warnf(format string, args ...interface{})  {}
func (l *aclTestLogger) Error(msg string)                          {}
func (l *aclTestLogger) Errorf(format string, args ...interface{}) {}
func (l *aclTestLogger) Fatal(msg string)                          {}
func (l *aclTestLogger) Fatalf(format string, args ...interface{}) {}
func (l *aclTestLogger) WithField(key string, value interface{}) interfaces.Logger {
	return l
}
func (l *aclTestLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	return l
}
func (l *aclTestLogger) WithError(err error) interfaces.Logger {
	return l
}

// createACLTestEntry creates a test entry with ACL.
func createACLTestEntry(key, owner string, acl *ObjectACL) *CatalogEntry {
	return &CatalogEntry{
		Key:      key,
		Owner:    owner,
		TenantID: "tenant1",
		Size:     1024,
		Checksum: []byte("checksum"),
		PrimaryStore: StoreLocation{
			Store:     "local",
			ObjectRef: "/data/" + key,
			Status:    LocationStatusActive,
		},
		Status:    StatusActive,
		Version:   1,
		CreatedAt: time.Now().UnixNano(),
		ACL:       acl,
	}
}

// TestObjectAction tests ObjectAction type methods.
func TestObjectAction(t *testing.T) {
	t.Run("String", func(t *testing.T) {
		action := ActionRead
		if action.String() != "read" {
			t.Errorf("expected 'read', got %s", action.String())
		}
	})

	t.Run("IsValid_ValidActions", func(t *testing.T) {
		validActions := []ObjectAction{
			ActionRead, ActionWrite, ActionDelete, ActionAdmin,
			ActionReplicate, ActionArchive, ActionList, ActionVerify,
		}
		for _, action := range validActions {
			if !action.IsValid() {
				t.Errorf("expected %s to be valid", action)
			}
		}
	})

	t.Run("IsValid_InvalidAction", func(t *testing.T) {
		action := ObjectAction("invalid")
		if action.IsValid() {
			t.Error("expected invalid action to be invalid")
		}
	})

	t.Run("AllActions", func(t *testing.T) {
		actions := AllActions()
		if len(actions) != 8 {
			t.Errorf("expected 8 actions, got %d", len(actions))
		}
	})
}

// TestNewACLManager tests ACL manager creation.
func TestNewACLManager(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{
			Catalog: catalog,
			Logger:  &aclTestLogger{},
		}
		manager, err := NewACLManager(config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if manager == nil {
			t.Fatal("expected manager to be non-nil")
		}
	})

	t.Run("NilConfig", func(t *testing.T) {
		_, err := NewACLManager(nil)
		if err != ErrConfigNil {
			t.Errorf("expected ErrConfigNil, got %v", err)
		}
	})

	t.Run("NilCatalog", func(t *testing.T) {
		config := &ACLManagerConfig{
			Catalog: nil,
		}
		_, err := NewACLManager(config)
		if err != ErrCatalogNil {
			t.Errorf("expected ErrCatalogNil, got %v", err)
		}
	})

	t.Run("WithNamespaceACLs", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		nsACLs := map[string]*NamespaceACL{
			"ns1": {
				Namespace:      "ns1",
				Owner:          "admin",
				DefaultReaders: []string{"reader1"},
			},
		}
		config := &ACLManagerConfig{
			Catalog:       catalog,
			Logger:        &aclTestLogger{},
			NamespaceACLs: nsACLs,
		}
		manager, err := NewACLManager(config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if manager == nil {
			t.Fatal("expected manager to be non-nil")
		}
	})
}

// TestCheckAccess tests access checking functionality.
func TestCheckAccess(t *testing.T) {
	ctx := context.Background()

	t.Run("OwnerHasFullAccess", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		allowed, err := manager.CheckAccess(ctx, "owner1", "obj1", ActionAdmin)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Error("owner should have full access")
		}
	})

	t.Run("ACLOwnerHasFullAccess", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		acl := &ObjectACL{Owner: "acl-owner"}
		entry := createACLTestEntry("obj1", "entry-owner", acl)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		allowed, err := manager.CheckAccess(ctx, "acl-owner", "obj1", ActionAdmin)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Error("ACL owner should have full access")
		}
	})

	t.Run("NoACLOnlyOwnerHasAccess", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		// Non-owner should be denied
		allowed, err := manager.CheckAccess(ctx, "other-user", "obj1", ActionRead)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if allowed {
			t.Error("non-owner should not have access without ACL")
		}
	})

	t.Run("PublicRead", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		acl := &ObjectACL{Owner: "owner1", PublicRead: true}
		entry := createACLTestEntry("obj1", "owner1", acl)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		// Anonymous should be able to read
		allowed, err := manager.CheckAccess(ctx, "anonymous", "obj1", ActionRead)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Error("public read should allow anonymous read")
		}

		// But not write
		allowed, err = manager.CheckAccess(ctx, "anonymous", "obj1", ActionWrite)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if allowed {
			t.Error("public read should not allow anonymous write")
		}
	})

	t.Run("AdminsHaveFullAccess", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		acl := &ObjectACL{Owner: "owner1", Admins: []string{"admin1", "admin2"}}
		entry := createACLTestEntry("obj1", "owner1", acl)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		// Admin should have full access
		actions := []ObjectAction{ActionRead, ActionWrite, ActionDelete, ActionAdmin}
		for _, action := range actions {
			allowed, err := manager.CheckAccess(ctx, "admin1", "obj1", action)
			if err != nil {
				t.Fatalf("unexpected error for action %s: %v", action, err)
			}
			if !allowed {
				t.Errorf("admin should have access for action %s", action)
			}
		}
	})

	t.Run("ReadersHaveReadAccess", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		acl := &ObjectACL{Owner: "owner1", Readers: []string{"reader1"}}
		entry := createACLTestEntry("obj1", "owner1", acl)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		// Reader should be able to read
		allowed, err := manager.CheckAccess(ctx, "reader1", "obj1", ActionRead)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Error("reader should have read access")
		}

		// But not write
		allowed, err = manager.CheckAccess(ctx, "reader1", "obj1", ActionWrite)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if allowed {
			t.Error("reader should not have write access")
		}
	})

	t.Run("WritersHaveWriteAndReadAccess", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		acl := &ObjectACL{Owner: "owner1", Writers: []string{"writer1"}}
		entry := createACLTestEntry("obj1", "owner1", acl)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		// Writer should be able to write
		allowed, err := manager.CheckAccess(ctx, "writer1", "obj1", ActionWrite)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Error("writer should have write access")
		}

		// And read (writers also get read)
		allowed, err = manager.CheckAccess(ctx, "writer1", "obj1", ActionRead)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Error("writer should have read access")
		}

		// But not delete
		allowed, err = manager.CheckAccess(ctx, "writer1", "obj1", ActionDelete)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if allowed {
			t.Error("writer should not have delete access")
		}
	})

	t.Run("InvalidAction", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		_, err := manager.CheckAccess(ctx, "owner1", "obj1", ObjectAction("invalid"))
		if err == nil {
			t.Error("expected error for invalid action")
		}
		var ce *CatalogError
		if errors.As(err, &ce) {
			if ce.Code != ErrCodeACLInvalidAction {
				t.Errorf("expected ErrCodeACLInvalidAction, got %s", ce.Code)
			}
		}
	})

	t.Run("EmptyKey", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		_, err := manager.CheckAccess(ctx, "user1", "", ActionRead)
		if err == nil {
			t.Error("expected error for empty key")
		}
	})

	t.Run("EntryNotFound", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		_, err := manager.CheckAccess(ctx, "user1", "nonexistent", ActionRead)
		if !IsNotFound(err) {
			t.Error("expected not found error")
		}
	})

	t.Run("ReplicateAction", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		acl := &ObjectACL{Owner: "owner1", Writers: []string{"writer1"}}
		entry := createACLTestEntry("obj1", "owner1", acl)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		// Writers can replicate
		allowed, err := manager.CheckAccess(ctx, "writer1", "obj1", ActionReplicate)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Error("writer should have replicate access")
		}
	})

	t.Run("VerifyRequiresAdmin", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		acl := &ObjectACL{Owner: "owner1", Writers: []string{"writer1"}}
		entry := createACLTestEntry("obj1", "owner1", acl)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		// Writers cannot verify
		allowed, err := manager.CheckAccess(ctx, "writer1", "obj1", ActionVerify)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if allowed {
			t.Error("writer should not have verify access")
		}
	})

	t.Run("ArchiveActionForWriter", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		acl := &ObjectACL{Owner: "owner1", Writers: []string{"writer1"}}
		entry := createACLTestEntry("obj1", "owner1", acl)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		// Writers can archive
		allowed, err := manager.CheckAccess(ctx, "writer1", "obj1", ActionArchive)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Error("writer should have archive access")
		}
	})

	t.Run("ListActionForReader", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		acl := &ObjectACL{Owner: "owner1", Readers: []string{"reader1"}}
		entry := createACLTestEntry("obj1", "owner1", acl)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		// Readers can list
		allowed, err := manager.CheckAccess(ctx, "reader1", "obj1", ActionList)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Error("reader should have list access")
		}
	})

	t.Run("DefaultSwitchCaseDenied", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		acl := &ObjectACL{Owner: "owner1", Readers: []string{"reader1"}}
		entry := createACLTestEntry("obj1", "owner1", acl)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		// Reader should not have admin access (falls through default case denial)
		allowed, err := manager.CheckAccess(ctx, "reader1", "obj1", ActionAdmin)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if allowed {
			t.Error("reader should not have admin access")
		}
	})
}

// TestSetACL tests setting ACLs on objects.
func TestSetACL(t *testing.T) {
	ctx := context.Background()

	t.Run("SetValidACL", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		newACL := &ObjectACL{
			Owner:   "owner1",
			Readers: []string{"reader1"},
		}
		err := manager.SetACL(ctx, "obj1", newACL)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify ACL was set
		acl, err := manager.GetACL(ctx, "obj1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(acl.Readers) != 1 || acl.Readers[0] != "reader1" {
			t.Error("ACL was not set correctly")
		}
	})

	t.Run("EmptyKey", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.SetACL(ctx, "", &ObjectACL{})
		if err == nil {
			t.Error("expected error for empty key")
		}
	})

	t.Run("NilACL", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.SetACL(ctx, "obj1", nil)
		if err != ErrACLNil {
			t.Errorf("expected ErrACLNil, got %v", err)
		}
	})
}

// TestGetACL tests retrieving ACLs.
func TestGetACL(t *testing.T) {
	ctx := context.Background()

	t.Run("GetExistingACL", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		acl := &ObjectACL{Owner: "owner1", Readers: []string{"reader1"}}
		entry := createACLTestEntry("obj1", "owner1", acl)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		result, err := manager.GetACL(ctx, "obj1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Owner != "owner1" {
			t.Errorf("expected owner 'owner1', got %s", result.Owner)
		}
	})

	t.Run("GetDefaultACLWhenNone", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		result, err := manager.GetACL(ctx, "obj1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should return default ACL with entry owner
		if result.Owner != "owner1" {
			t.Errorf("expected owner 'owner1', got %s", result.Owner)
		}
	})

	t.Run("EmptyKey", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		_, err := manager.GetACL(ctx, "")
		if err == nil {
			t.Error("expected error for empty key")
		}
	})

	t.Run("GetACLEntryNotFound", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		_, err := manager.GetACL(ctx, "nonexistent")
		if !IsNotFound(err) {
			t.Error("expected not found error")
		}
	})
}

// TestGrantAccess tests granting access to principals.
func TestGrantAccess(t *testing.T) {
	ctx := context.Background()

	t.Run("GrantReadAccess", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.GrantAccess(ctx, "obj1", "user1", []ObjectAction{ActionRead})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify access was granted
		allowed, err := manager.CheckAccess(ctx, "user1", "obj1", ActionRead)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Error("user should have read access after grant")
		}
	})

	t.Run("GrantWriteAccess", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.GrantAccess(ctx, "obj1", "user1", []ObjectAction{ActionWrite})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify access was granted
		allowed, err := manager.CheckAccess(ctx, "user1", "obj1", ActionWrite)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Error("user should have write access after grant")
		}
	})

	t.Run("GrantAdminAccess", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.GrantAccess(ctx, "obj1", "user1", []ObjectAction{ActionAdmin})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify admin access was granted
		allowed, err := manager.CheckAccess(ctx, "user1", "obj1", ActionDelete)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Error("admin should have delete access")
		}
	})

	t.Run("EmptyKey", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.GrantAccess(ctx, "", "user1", []ObjectAction{ActionRead})
		if err == nil {
			t.Error("expected error for empty key")
		}
	})

	t.Run("EmptyPrincipal", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.GrantAccess(ctx, "obj1", "", []ObjectAction{ActionRead})
		if err == nil {
			t.Error("expected error for empty principal")
		}
	})

	t.Run("EmptyPermissions", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.GrantAccess(ctx, "obj1", "user1", []ObjectAction{})
		if err == nil {
			t.Error("expected error for empty permissions")
		}
	})

	t.Run("InvalidPermission", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.GrantAccess(ctx, "obj1", "user1", []ObjectAction{ObjectAction("invalid")})
		if err == nil {
			t.Error("expected error for invalid permission")
		}
	})

	t.Run("GrantListAction", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.GrantAccess(ctx, "obj1", "user1", []ObjectAction{ActionList})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify access was granted (list grants reader)
		allowed, err := manager.CheckAccess(ctx, "user1", "obj1", ActionList)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Error("user should have list access after grant")
		}
	})

	t.Run("GrantReplicateAction", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.GrantAccess(ctx, "obj1", "user1", []ObjectAction{ActionReplicate})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify access was granted (replicate grants writer)
		allowed, err := manager.CheckAccess(ctx, "user1", "obj1", ActionReplicate)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Error("user should have replicate access after grant")
		}
	})

	t.Run("GrantArchiveAction", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.GrantAccess(ctx, "obj1", "user1", []ObjectAction{ActionArchive})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify access was granted (archive grants writer)
		allowed, err := manager.CheckAccess(ctx, "user1", "obj1", ActionArchive)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Error("user should have archive access after grant")
		}
	})

	t.Run("GrantDeleteAction", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.GrantAccess(ctx, "obj1", "user1", []ObjectAction{ActionDelete})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify access was granted (delete grants admin)
		allowed, err := manager.CheckAccess(ctx, "user1", "obj1", ActionDelete)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Error("user should have delete access after grant")
		}
	})

	t.Run("GrantVerifyAction", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.GrantAccess(ctx, "obj1", "user1", []ObjectAction{ActionVerify})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify access was granted (verify grants admin)
		allowed, err := manager.CheckAccess(ctx, "user1", "obj1", ActionVerify)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Error("user should have verify access after grant")
		}
	})
}

// TestRevokeAccess tests revoking access from principals.
func TestRevokeAccess(t *testing.T) {
	ctx := context.Background()

	t.Run("RevokeAccess", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		acl := &ObjectACL{
			Owner:   "owner1",
			Readers: []string{"reader1"},
			Writers: []string{"reader1"},
		}
		entry := createACLTestEntry("obj1", "owner1", acl)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		// Verify initial access
		allowed, _ := manager.CheckAccess(ctx, "reader1", "obj1", ActionRead)
		if !allowed {
			t.Fatal("reader should initially have access")
		}

		// Revoke access
		err := manager.RevokeAccess(ctx, "obj1", "reader1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify access was revoked
		allowed, _ = manager.CheckAccess(ctx, "reader1", "obj1", ActionRead)
		if allowed {
			t.Error("reader should not have access after revoke")
		}
	})

	t.Run("CannotRevokeOwner", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		acl := &ObjectACL{Owner: "owner1"}
		entry := createACLTestEntry("obj1", "owner1", acl)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.RevokeAccess(ctx, "obj1", "owner1")
		if err == nil {
			t.Error("expected error when revoking owner access")
		}
		var ce *CatalogError
		if errors.As(err, &ce) {
			if ce.Code != ErrCodeACLCannotRevokeOwner {
				t.Errorf("expected ErrCodeACLCannotRevokeOwner, got %s", ce.Code)
			}
		}
	})

	t.Run("RevokeNonExistentAccess", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		acl := &ObjectACL{Owner: "owner1"}
		entry := createACLTestEntry("obj1", "owner1", acl)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		// Revoking non-existent access should succeed (no-op)
		err := manager.RevokeAccess(ctx, "obj1", "nonexistent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("EmptyKey", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.RevokeAccess(ctx, "", "user1")
		if err == nil {
			t.Error("expected error for empty key")
		}
	})

	t.Run("EmptyPrincipal", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.RevokeAccess(ctx, "obj1", "")
		if err == nil {
			t.Error("expected error for empty principal")
		}
	})

	t.Run("RevokeAccessNilACL", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		// Revoking from an entry with nil ACL should succeed (no-op)
		err := manager.RevokeAccess(ctx, "obj1", "someuser")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// errorCatalogManager is a mock that returns errors on Search
type errorCatalogManager struct {
	*aclTestCatalogManager
	searchError error
}

func newErrorCatalogManager() *errorCatalogManager {
	return &errorCatalogManager{
		aclTestCatalogManager: newACLTestCatalogManager(),
		searchError:           errors.New("search error"),
	}
}

func (m *errorCatalogManager) Search(ctx context.Context, query *SearchQuery) (*SearchResult, error) {
	if m.searchError != nil {
		return nil, m.searchError
	}
	return m.aclTestCatalogManager.Search(ctx, query)
}

// TestListAccessible tests listing accessible objects.
func TestListAccessible(t *testing.T) {
	ctx := context.Background()

	t.Run("ListAccessibleObjects", func(t *testing.T) {
		catalog := newACLTestCatalogManager()

		// Create objects with different access
		entry1 := createACLTestEntry("obj1", "user1", nil) // user1 owns
		entry2 := createACLTestEntry("obj2", "user2", &ObjectACL{
			Owner:   "user2",
			Readers: []string{"user1"}, // user1 can read
		})
		entry3 := createACLTestEntry("obj3", "user3", nil) // user1 cannot access

		catalog.Put(ctx, entry1)
		catalog.Put(ctx, entry2)
		catalog.Put(ctx, entry3)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		result, err := manager.ListAccessible(ctx, "user1", ActionRead, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// user1 should be able to read obj1 (owner) and obj2 (reader)
		if len(result.Keys) != 2 {
			t.Errorf("expected 2 accessible objects, got %d", len(result.Keys))
		}
	})

	t.Run("EmptyPrincipal", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		_, err := manager.ListAccessible(ctx, "", ActionRead, nil)
		if err == nil {
			t.Error("expected error for empty principal")
		}
	})

	t.Run("InvalidAction", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		_, err := manager.ListAccessible(ctx, "user1", ObjectAction("invalid"), nil)
		if err == nil {
			t.Error("expected error for invalid action")
		}
	})

	t.Run("ListAccessibleWithLimit", func(t *testing.T) {
		catalog := newACLTestCatalogManager()

		// Create multiple objects owned by user1
		for i := 0; i < 5; i++ {
			entry := createACLTestEntry("obj"+string(rune('a'+i)), "user1", nil)
			catalog.Put(ctx, entry)
		}

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		result, err := manager.ListAccessible(ctx, "user1", ActionRead, &ListOptions{Limit: 3})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result.Keys) != 3 {
			t.Errorf("expected 3 accessible objects with limit, got %d", len(result.Keys))
		}
		if !result.HasMore {
			t.Error("expected HasMore to be true when more results exist")
		}
	})

	t.Run("ListAccessibleSearchError", func(t *testing.T) {
		catalog := newErrorCatalogManager()

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		_, err := manager.ListAccessible(ctx, "user1", ActionRead, nil)
		if err == nil {
			t.Error("expected error when search fails")
		}
	})
}

// TestNamespaceACL tests namespace-level ACL functionality.
func TestNamespaceACL(t *testing.T) {
	ctx := context.Background()

	t.Run("GetNamespaceACL", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		nsACLs := map[string]*NamespaceACL{
			"ns1": {
				Namespace:      "ns1",
				Owner:          "admin",
				DefaultReaders: []string{"reader1"},
			},
		}
		config := &ACLManagerConfig{
			Catalog:       catalog,
			NamespaceACLs: nsACLs,
		}
		manager, _ := NewACLManager(config)

		acl, err := manager.GetNamespaceACL(ctx, "ns1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if acl.Owner != "admin" {
			t.Errorf("expected owner 'admin', got %s", acl.Owner)
		}
	})

	t.Run("GetNonExistentNamespaceACL", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		acl, err := manager.GetNamespaceACL(ctx, "ns1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should return empty ACL with namespace set
		if acl.Namespace != "ns1" {
			t.Errorf("expected namespace 'ns1', got %s", acl.Namespace)
		}
	})

	t.Run("SetNamespaceACL", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		nsACL := &NamespaceACL{
			Namespace:      "ns1",
			Owner:          "admin",
			DefaultReaders: []string{"reader1"},
		}
		err := manager.SetNamespaceACL(ctx, "ns1", nsACL)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify it was set
		acl, _ := manager.GetNamespaceACL(ctx, "ns1")
		if acl.Owner != "admin" {
			t.Errorf("expected owner 'admin', got %s", acl.Owner)
		}
	})

	t.Run("EmptyNamespace", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		_, err := manager.GetNamespaceACL(ctx, "")
		if err == nil {
			t.Error("expected error for empty namespace")
		}
	})

	t.Run("SetNamespaceACLEmptyNamespace", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		nsACL := &NamespaceACL{Namespace: "ns1", Owner: "admin"}
		err := manager.SetNamespaceACL(ctx, "", nsACL)
		if err == nil {
			t.Error("expected error for empty namespace")
		}
	})

	t.Run("SetNamespaceACLNilACL", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.SetNamespaceACL(ctx, "ns1", nil)
		if err != ErrACLNil {
			t.Errorf("expected ErrACLNil, got %v", err)
		}
	})
}

// TestTransferOwnership tests ownership transfer.
func TestTransferOwnership(t *testing.T) {
	ctx := context.Background()

	t.Run("TransferOwnership", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		acl := &ObjectACL{Owner: "owner1"}
		entry := createACLTestEntry("obj1", "owner1", acl)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.TransferOwnership(ctx, "obj1", "newowner")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify new owner has access
		allowed, _ := manager.CheckAccess(ctx, "newowner", "obj1", ActionAdmin)
		if !allowed {
			t.Error("new owner should have admin access")
		}

		// Verify old owner is now admin (not owner)
		resultACL, _ := manager.GetACL(ctx, "obj1")
		if resultACL.Owner != "newowner" {
			t.Errorf("expected new owner, got %s", resultACL.Owner)
		}
	})

	t.Run("EmptyKey", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.TransferOwnership(ctx, "", "newowner")
		if err == nil {
			t.Error("expected error for empty key")
		}
	})

	t.Run("EmptyNewOwner", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.TransferOwnership(ctx, "obj1", "")
		if err == nil {
			t.Error("expected error for empty new owner")
		}
	})

	t.Run("TransferOwnershipWithoutACL", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		entry := createACLTestEntry("obj1", "owner1", nil)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.TransferOwnership(ctx, "obj1", "newowner")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify new owner has access
		allowed, _ := manager.CheckAccess(ctx, "newowner", "obj1", ActionAdmin)
		if !allowed {
			t.Error("new owner should have admin access")
		}
	})

	t.Run("TransferOwnershipToSameOwner", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		acl := &ObjectACL{Owner: "owner1"}
		entry := createACLTestEntry("obj1", "owner1", acl)
		catalog.Put(ctx, entry)

		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.TransferOwnership(ctx, "obj1", "owner1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify owner is still the same
		resultACL, _ := manager.GetACL(ctx, "obj1")
		if resultACL.Owner != "owner1" {
			t.Errorf("expected owner to be 'owner1', got %s", resultACL.Owner)
		}
	})

	t.Run("TransferOwnershipEntryNotFound", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		err := manager.TransferOwnership(ctx, "nonexistent", "newowner")
		if !IsNotFound(err) {
			t.Error("expected not found error")
		}
	})
}

// TestBatchCheckAccess tests batch access checking.
func TestBatchCheckAccess(t *testing.T) {
	ctx := context.Background()

	t.Run("BatchCheckAccess", func(t *testing.T) {
		catalog := newACLTestCatalogManager()

		entry1 := createACLTestEntry("obj1", "user1", nil)
		entry2 := createACLTestEntry("obj2", "user1", nil)
		entry3 := createACLTestEntry("obj3", "user2", nil)

		catalog.Put(ctx, entry1)
		catalog.Put(ctx, entry2)
		catalog.Put(ctx, entry3)

		config := &ACLManagerConfig{Catalog: catalog, Logger: &aclTestLogger{}}
		manager, _ := NewACLManager(config)

		results, err := manager.BatchCheckAccess(ctx, "user1", []string{"obj1", "obj2", "obj3"}, ActionRead)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !results["obj1"] {
			t.Error("user1 should have access to obj1")
		}
		if !results["obj2"] {
			t.Error("user1 should have access to obj2")
		}
		if results["obj3"] {
			t.Error("user1 should not have access to obj3")
		}
	})

	t.Run("EmptyPrincipal", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		_, err := manager.BatchCheckAccess(ctx, "", []string{"obj1"}, ActionRead)
		if err == nil {
			t.Error("expected error for empty principal")
		}
	})

	t.Run("EmptyKeys", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		results, err := manager.BatchCheckAccess(ctx, "user1", []string{}, ActionRead)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(results) != 0 {
			t.Error("expected empty results for empty keys")
		}
	})

	t.Run("InvalidAction", func(t *testing.T) {
		catalog := newACLTestCatalogManager()
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		_, err := manager.BatchCheckAccess(ctx, "user1", []string{"obj1"}, ObjectAction("invalid"))
		if err == nil {
			t.Error("expected error for invalid action")
		}
	})

	t.Run("BatchCheckAccessWithMissingKey", func(t *testing.T) {
		catalog := newACLTestCatalogManager()

		entry1 := createACLTestEntry("obj1", "user1", nil)
		catalog.Put(ctx, entry1)

		config := &ACLManagerConfig{Catalog: catalog, Logger: &aclTestLogger{}}
		manager, _ := NewACLManager(config)

		results, err := manager.BatchCheckAccess(ctx, "user1", []string{"obj1", "nonexistent"}, ActionRead)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !results["obj1"] {
			t.Error("user1 should have access to obj1")
		}
		if results["nonexistent"] {
			t.Error("nonexistent should be denied")
		}
	})

	t.Run("BatchCheckAccessWithoutLogger", func(t *testing.T) {
		catalog := newACLTestCatalogManager()

		entry1 := createACLTestEntry("obj1", "user1", nil)
		catalog.Put(ctx, entry1)

		// No logger configured
		config := &ACLManagerConfig{Catalog: catalog}
		manager, _ := NewACLManager(config)

		results, err := manager.BatchCheckAccess(ctx, "user1", []string{"obj1", "nonexistent"}, ActionRead)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !results["obj1"] {
			t.Error("user1 should have access to obj1")
		}
		if results["nonexistent"] {
			t.Error("nonexistent should be denied")
		}
	})
}

// TestObjectACLHelpers tests ObjectACL helper methods.
func TestObjectACLHelpers(t *testing.T) {
	t.Run("Clone", func(t *testing.T) {
		acl := &ObjectACL{
			Owner:      "owner1",
			Readers:    []string{"reader1"},
			Writers:    []string{"writer1"},
			Admins:     []string{"admin1"},
			PublicRead: true,
		}
		clone := acl.Clone()

		// Modify original
		acl.Readers[0] = "modified"

		// Clone should be unchanged
		if clone.Readers[0] != "reader1" {
			t.Error("clone should be independent of original")
		}
	})

	t.Run("Validate", func(t *testing.T) {
		// ACL with owner is valid
		acl := &ObjectACL{Owner: "owner1"}
		if err := acl.Validate(); err != nil {
			t.Errorf("expected valid ACL, got error: %v", err)
		}

		// ACL with public read and no owner is valid
		acl2 := &ObjectACL{PublicRead: true}
		if err := acl2.Validate(); err != nil {
			t.Errorf("expected valid ACL with public read, got error: %v", err)
		}

		// ACL with inherit and no owner is valid
		acl3 := &ObjectACL{Inherit: true}
		if err := acl3.Validate(); err != nil {
			t.Errorf("expected valid ACL with inherit, got error: %v", err)
		}

		// ACL without owner, public read, or inherit is invalid
		acl4 := &ObjectACL{}
		if err := acl4.Validate(); err == nil {
			t.Error("expected error for ACL without owner")
		}
	})

	t.Run("ValidateNilACL", func(t *testing.T) {
		var acl *ObjectACL
		if err := acl.Validate(); err != nil {
			t.Errorf("expected nil ACL to be valid (returns nil), got error: %v", err)
		}
	})

	t.Run("ApplyNamespaceDefaults", func(t *testing.T) {
		acl := &ObjectACL{Owner: "owner1"}
		nsACL := &NamespaceACL{
			DefaultReaders: []string{"reader1"},
			DefaultWriters: []string{"writer1"},
			DefaultAdmins:  []string{"admin1"},
			PublicRead:     true,
		}

		acl.ApplyNamespaceDefaults(nsACL)

		if len(acl.Readers) != 1 || acl.Readers[0] != "reader1" {
			t.Error("default readers not applied")
		}
		if len(acl.Writers) != 1 || acl.Writers[0] != "writer1" {
			t.Error("default writers not applied")
		}
		if len(acl.Admins) != 1 || acl.Admins[0] != "admin1" {
			t.Error("default admins not applied")
		}
		if !acl.PublicRead {
			t.Error("public read not applied")
		}
	})

	t.Run("ApplyNamespaceDefaultsNilACL", func(t *testing.T) {
		var acl *ObjectACL
		nsACL := &NamespaceACL{DefaultReaders: []string{"reader1"}}
		// Should not panic
		acl.ApplyNamespaceDefaults(nsACL)
	})

	t.Run("ApplyNamespaceDefaultsNilNsACL", func(t *testing.T) {
		acl := &ObjectACL{Owner: "owner1"}
		// Should not panic
		acl.ApplyNamespaceDefaults(nil)
	})

	t.Run("ApplyNamespaceDefaultsWithoutPublicRead", func(t *testing.T) {
		acl := &ObjectACL{Owner: "owner1", PublicRead: false}
		nsACL := &NamespaceACL{
			DefaultReaders: []string{"reader1"},
			PublicRead:     false,
		}

		acl.ApplyNamespaceDefaults(nsACL)

		if acl.PublicRead {
			t.Error("public read should remain false")
		}
	})

	t.Run("Merge", func(t *testing.T) {
		acl1 := &ObjectACL{
			Owner:   "owner1",
			Readers: []string{"reader1"},
		}
		acl2 := &ObjectACL{
			Readers:    []string{"reader2"},
			Writers:    []string{"writer1"},
			PublicRead: true,
		}

		acl1.Merge(acl2)

		if len(acl1.Readers) != 2 {
			t.Error("readers not merged")
		}
		if len(acl1.Writers) != 1 {
			t.Error("writers not merged")
		}
		if !acl1.PublicRead {
			t.Error("public read not merged")
		}
	})

	t.Run("MergeNilACL", func(t *testing.T) {
		var acl *ObjectACL
		other := &ObjectACL{Readers: []string{"reader1"}}
		// Should not panic
		acl.Merge(other)
	})

	t.Run("MergeNilOther", func(t *testing.T) {
		acl := &ObjectACL{Owner: "owner1"}
		// Should not panic
		acl.Merge(nil)
		if acl.Owner != "owner1" {
			t.Error("ACL should be unchanged")
		}
	})

	t.Run("MergeWithAdmins", func(t *testing.T) {
		acl1 := &ObjectACL{
			Owner:  "owner1",
			Admins: []string{"admin1"},
		}
		acl2 := &ObjectACL{
			Admins: []string{"admin2"},
		}

		acl1.Merge(acl2)

		if len(acl1.Admins) != 2 {
			t.Errorf("expected 2 admins, got %d", len(acl1.Admins))
		}
	})

	t.Run("MergeWithoutPublicRead", func(t *testing.T) {
		acl1 := &ObjectACL{
			Owner:      "owner1",
			PublicRead: true,
		}
		acl2 := &ObjectACL{
			PublicRead: false,
		}

		acl1.Merge(acl2)

		// PublicRead is only set to true if other.PublicRead is true
		if !acl1.PublicRead {
			t.Error("public read should remain true")
		}
	})

	t.Run("IsEmpty", func(t *testing.T) {
		acl := &ObjectACL{Owner: "owner1"}
		if !acl.IsEmpty() {
			t.Error("ACL with only owner should be empty")
		}

		acl.Readers = []string{"reader1"}
		if acl.IsEmpty() {
			t.Error("ACL with readers should not be empty")
		}
	})

	t.Run("IsEmptyNilACL", func(t *testing.T) {
		var acl *ObjectACL
		if !acl.IsEmpty() {
			t.Error("nil ACL should be empty")
		}
	})

	t.Run("IsEmptyWithWritersOnly", func(t *testing.T) {
		acl := &ObjectACL{Owner: "owner1", Writers: []string{"writer1"}}
		if acl.IsEmpty() {
			t.Error("ACL with writers should not be empty")
		}
	})

	t.Run("IsEmptyWithAdminsOnly", func(t *testing.T) {
		acl := &ObjectACL{Owner: "owner1", Admins: []string{"admin1"}}
		if acl.IsEmpty() {
			t.Error("ACL with admins should not be empty")
		}
	})

	t.Run("IsEmptyWithPublicRead", func(t *testing.T) {
		acl := &ObjectACL{Owner: "owner1", PublicRead: true}
		if acl.IsEmpty() {
			t.Error("ACL with public read should not be empty")
		}
	})

	t.Run("GetAllPrincipals", func(t *testing.T) {
		acl := &ObjectACL{
			Owner:   "owner1",
			Readers: []string{"reader1", "owner1"}, // owner1 duplicated
			Writers: []string{"writer1"},
			Admins:  []string{"admin1"},
		}

		principals := acl.GetAllPrincipals()
		// Should deduplicate
		if len(principals) != 4 {
			t.Errorf("expected 4 unique principals, got %d", len(principals))
		}
	})

	t.Run("GetAllPrincipalsNilACL", func(t *testing.T) {
		var acl *ObjectACL
		principals := acl.GetAllPrincipals()
		if principals != nil {
			t.Error("expected nil principals for nil ACL")
		}
	})

	t.Run("GetAllPrincipalsNoOwner", func(t *testing.T) {
		acl := &ObjectACL{
			Readers: []string{"reader1"},
			Writers: []string{"writer1"},
		}

		principals := acl.GetAllPrincipals()
		if len(principals) != 2 {
			t.Errorf("expected 2 principals, got %d", len(principals))
		}
	})

	t.Run("GetEffectivePermissions_Owner", func(t *testing.T) {
		acl := &ObjectACL{Owner: "owner1"}
		perms := acl.GetEffectivePermissions("owner1")
		if len(perms) != 8 {
			t.Errorf("owner should have all 8 permissions, got %d", len(perms))
		}
	})

	t.Run("GetEffectivePermissions_Admin", func(t *testing.T) {
		acl := &ObjectACL{Owner: "owner1", Admins: []string{"admin1"}}
		perms := acl.GetEffectivePermissions("admin1")
		if len(perms) != 8 {
			t.Errorf("admin should have all 8 permissions, got %d", len(perms))
		}
	})

	t.Run("GetEffectivePermissions_Reader", func(t *testing.T) {
		acl := &ObjectACL{Owner: "owner1", Readers: []string{"reader1"}}
		perms := acl.GetEffectivePermissions("reader1")
		if len(perms) != 2 {
			t.Errorf("reader should have 2 permissions, got %d", len(perms))
		}
	})

	t.Run("GetEffectivePermissions_Writer", func(t *testing.T) {
		acl := &ObjectACL{Owner: "owner1", Writers: []string{"writer1"}}
		perms := acl.GetEffectivePermissions("writer1")
		// read, write, replicate, archive
		if len(perms) != 4 {
			t.Errorf("writer should have 4 permissions, got %d", len(perms))
		}
	})

	t.Run("GetEffectivePermissions_PublicRead", func(t *testing.T) {
		acl := &ObjectACL{Owner: "owner1", PublicRead: true}
		perms := acl.GetEffectivePermissions("anonymous")
		if len(perms) != 1 {
			t.Errorf("anonymous with public read should have 1 permission, got %d", len(perms))
		}
	})

	t.Run("GetEffectivePermissions_NilACL", func(t *testing.T) {
		var acl *ObjectACL
		perms := acl.GetEffectivePermissions("anyone")
		if perms != nil {
			t.Error("expected nil permissions for nil ACL")
		}
	})

	t.Run("GetEffectivePermissions_ReaderAndWriter", func(t *testing.T) {
		acl := &ObjectACL{
			Owner:   "owner1",
			Readers: []string{"user1"},
			Writers: []string{"user1"},
		}
		perms := acl.GetEffectivePermissions("user1")
		// read, list from reader; read (deduped), write, replicate, archive from writer
		// Total: read, list, write, replicate, archive = 5
		if len(perms) != 5 {
			t.Errorf("reader+writer should have 5 permissions, got %d", len(perms))
		}
	})

	t.Run("GetEffectivePermissions_PublicReadAndWriter", func(t *testing.T) {
		acl := &ObjectACL{
			Owner:      "owner1",
			Writers:    []string{"user1"},
			PublicRead: true,
		}
		perms := acl.GetEffectivePermissions("user1")
		// public read gives read; writer gives write, replicate, archive (read already present)
		// Total: read, write, replicate, archive = 4
		if len(perms) != 4 {
			t.Errorf("public read + writer should have 4 permissions, got %d", len(perms))
		}
	})

	t.Run("GetEffectivePermissions_NoPrincipals", func(t *testing.T) {
		acl := &ObjectACL{Owner: "owner1"}
		perms := acl.GetEffectivePermissions("stranger")
		if len(perms) != 0 {
			t.Errorf("stranger should have 0 permissions, got %d", len(perms))
		}
	})
}

// TestNamespaceACLClone tests NamespaceACL cloning.
func TestNamespaceACLClone(t *testing.T) {
	t.Run("Clone", func(t *testing.T) {
		nsACL := &NamespaceACL{
			Namespace:         "ns1",
			Owner:             "admin",
			DefaultReaders:    []string{"reader1"},
			DefaultWriters:    []string{"writer1"},
			DefaultAdmins:     []string{"admin1"},
			PublicRead:        true,
			InheritFromTenant: true,
		}

		clone := nsACL.Clone()

		// Modify original
		nsACL.DefaultReaders[0] = "modified"

		// Clone should be unchanged
		if clone.DefaultReaders[0] != "reader1" {
			t.Error("clone should be independent of original")
		}
	})

	t.Run("CloneNil", func(t *testing.T) {
		var nsACL *NamespaceACL
		clone := nsACL.Clone()
		if clone != nil {
			t.Error("clone of nil should be nil")
		}
	})

	t.Run("CloneWithNilSlices", func(t *testing.T) {
		nsACL := &NamespaceACL{
			Namespace: "ns1",
			Owner:     "admin",
		}

		clone := nsACL.Clone()

		if clone.Namespace != "ns1" {
			t.Errorf("expected namespace 'ns1', got %s", clone.Namespace)
		}
		if clone.DefaultReaders != nil {
			t.Error("expected nil DefaultReaders")
		}
	})
}

// TestACLErrorHelpers tests ACL error helper functions.
func TestACLErrorHelpers(t *testing.T) {
	t.Run("IsACLError", func(t *testing.T) {
		aclErr := NewACLInvalidActionError("invalid")
		if !IsACLError(aclErr) {
			t.Error("expected IsACLError to return true")
		}

		nonACLErr := NewKeyEmptyError()
		if IsACLError(nonACLErr) {
			t.Error("expected IsACLError to return false for non-ACL error")
		}

		if IsACLError(nil) {
			t.Error("expected IsACLError to return false for nil")
		}
	})

	t.Run("IsAccessDenied", func(t *testing.T) {
		accessErr := NewACLAccessDeniedError("user1", "obj1", "read")
		if !IsAccessDenied(accessErr) {
			t.Error("expected IsAccessDenied to return true")
		}

		otherErr := NewKeyEmptyError()
		if IsAccessDenied(otherErr) {
			t.Error("expected IsAccessDenied to return false for non-access-denied error")
		}

		if IsAccessDenied(nil) {
			t.Error("expected IsAccessDenied to return false for nil")
		}
	})
}

// TestContainsHelper tests the contains helper function.
func TestContainsHelper(t *testing.T) {
	t.Run("Found", func(t *testing.T) {
		slice := []string{"a", "b", "c"}
		if !contains(slice, "b") {
			t.Error("expected to find 'b' in slice")
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		slice := []string{"a", "b", "c"}
		if contains(slice, "d") {
			t.Error("expected not to find 'd' in slice")
		}
	})

	t.Run("EmptySlice", func(t *testing.T) {
		var slice []string
		if contains(slice, "a") {
			t.Error("expected not to find anything in empty slice")
		}
	})
}
