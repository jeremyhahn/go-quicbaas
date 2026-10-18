// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package quicbaas

import (
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// =============================================================================
// Compile-Time Interface Assertions
// =============================================================================

// Verify that *client implements Client (which now includes sub-interface accessors).
var _ Client = (*client)(nil)

// Verify that wrapper types satisfy their sub-interfaces.
var _ CoreDataService = (*coreDataService)(nil)
var _ ClusterManagementService = (*clusterManagementService)(nil)
var _ SecurityService = (*securityService)(nil)
var _ IdentityService = (*identityService)(nil)
var _ SearchService = (*searchService)(nil)
var _ StorageService = (*storageService)(nil)
var _ AsyncService = (*asyncService)(nil)
var _ AIService = (*aiService)(nil)

// =============================================================================
// Test: Sub-Interface Accessors Return Non-Nil
// =============================================================================

func TestClient_SubInterfaceAccessors_ReturnNonNil(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{transport: mockT, opts: defaultOptions()}

	tests := []struct {
		name   string
		getter func() interface{}
	}{
		{"CoreData", func() interface{} { return c.CoreData() }},
		{"ClusterMgmt", func() interface{} { return c.ClusterMgmt() }},
		{"Security", func() interface{} { return c.Security() }},
		{"Identity", func() interface{} { return c.Identity() }},
		{"Search", func() interface{} { return c.Search() }},
		{"Storage", func() interface{} { return c.Storage() }},
		{"Async", func() interface{} { return c.Async() }},
		{"AI", func() interface{} { return c.AI() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.getter()
			if result == nil {
				t.Errorf("%s() returned nil, expected non-nil sub-interface", tt.name)
			}
		})
	}
}

// =============================================================================
// Test: CoreDataService Delegation
// =============================================================================

func TestCoreDataService_DelegatesToClient(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{transport: mockT, opts: defaultOptions()}
	svc := c.CoreData()

	t.Run("KV", func(t *testing.T) {
		kvFromSvc := svc.KV()
		kvFromClient := c.KV()
		if kvFromSvc != kvFromClient {
			t.Error("CoreData().KV() should return the same instance as client.KV()")
		}
	})

	t.Run("Batch", func(t *testing.T) {
		batchFromSvc := svc.Batch()
		batchFromClient := c.Batch()
		if batchFromSvc != batchFromClient {
			t.Error("CoreData().Batch() should return the same instance as client.Batch()")
		}
	})

	t.Run("Transaction", func(t *testing.T) {
		txnFromSvc := svc.Transaction()
		txnFromClient := c.Transaction()
		if txnFromSvc != txnFromClient {
			t.Error("CoreData().Transaction() should return the same instance as client.Transaction()")
		}
	})

	t.Run("Query", func(t *testing.T) {
		queryFromSvc := svc.Query()
		queryFromClient := c.Query()
		if queryFromSvc != queryFromClient {
			t.Error("CoreData().Query() should return the same instance as client.Query()")
		}
	})
}

// =============================================================================
// Test: ClusterManagementService Delegation
// =============================================================================

func TestClusterManagementService_DelegatesToClient(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{transport: mockT, opts: defaultOptions()}
	svc := c.ClusterMgmt()

	t.Run("Cluster", func(t *testing.T) {
		if svc.Cluster() != c.Cluster() {
			t.Error("ClusterMgmt().Cluster() should delegate to client.Cluster()")
		}
	})

	t.Run("Health", func(t *testing.T) {
		if svc.Health() != c.Health() {
			t.Error("ClusterMgmt().Health() should delegate to client.Health()")
		}
	})

	t.Run("Operations", func(t *testing.T) {
		if svc.Operations() != c.Operations() {
			t.Error("ClusterMgmt().Operations() should delegate to client.Operations()")
		}
	})

	t.Run("Cache", func(t *testing.T) {
		if svc.Cache() != c.Cache() {
			t.Error("ClusterMgmt().Cache() should delegate to client.Cache()")
		}
	})
}

// =============================================================================
// Test: SecurityService Delegation
// =============================================================================

func TestSecurityService_DelegatesToClient(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{transport: mockT, opts: defaultOptions()}
	svc := c.Security()

	t.Run("Auth", func(t *testing.T) {
		// Security().Auth() returns the extended AuthService (embeds
		// AuthClient + adds FIDO2/WebAuthn/PIV/Factors sub-services).
		// It does not return the same instance as c.auth() (the
		// narrower AuthClient): the extended service is constructed
		// lazily by securityService and wraps the client. The
		// contract is that the extended surface satisfies AuthClient,
		// which the type system enforces via interface embedding —
		// here we just assert non-nil construction.
		if svc.Auth() == nil {
			t.Error("Security().Auth() must return a non-nil AuthService")
		}
	})

	t.Run("KMS", func(t *testing.T) {
		if svc.KMS() != c.kms() {
			t.Error("Security().KMS() should delegate to client.KMS()")
		}
	})

	t.Run("Secrets", func(t *testing.T) {
		if svc.Secrets() != c.secrets() {
			t.Error("Security().Secrets() should delegate to client.Secrets()")
		}
	})

	t.Run("Crypto", func(t *testing.T) {
		if svc.Crypto() != c.crypto() {
			t.Error("Security().Crypto() should delegate to client.Crypto()")
		}
	})

	t.Run("Certificate", func(t *testing.T) {
		if svc.Certificate() != c.certificate() {
			t.Error("Security().Certificate() should delegate to client.Certificate()")
		}
	})

	t.Run("DynamicSecrets", func(t *testing.T) {
		if svc.DynamicSecrets() != c.dynamicSecrets() {
			t.Error("Security().DynamicSecrets() should delegate to client.DynamicSecrets()")
		}
	})

	t.Run("RBAC", func(t *testing.T) {
		if svc.RBAC() != c.rbac() {
			t.Error("Security().RBAC() should delegate to client.RBAC()")
		}
	})
}

// =============================================================================
// Test: IdentityService Delegation
// =============================================================================

func TestIdentityService_DelegatesToClient(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{transport: mockT, opts: defaultOptions()}
	svc := c.Identity()

	t.Run("IAM", func(t *testing.T) {
		// Identity().IAM() is the ONLY entry point: there is no flat
		// Client.IAM() to compare against (issue #905).
		if svc.IAM() != c.iam() {
			t.Error("Identity().IAM() should delegate to the client's IAM accessor")
		}
	})
}

// =============================================================================
// Test: SearchService Delegation
// =============================================================================

func TestSearchService_DelegatesToClient(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{transport: mockT, opts: defaultOptions()}
	svc := c.Search()

	t.Run("Vector", func(t *testing.T) {
		if svc.Vector() != c.Vector() {
			t.Error("Search().Vector() should delegate to client.Vector()")
		}
	})

	t.Run("TimeSeries", func(t *testing.T) {
		if svc.TimeSeries() != c.TimeSeries() {
			t.Error("Search().TimeSeries() should delegate to client.TimeSeries()")
		}
	})
}

// =============================================================================
// Test: StorageService Delegation
// =============================================================================

func TestStorageService_DelegatesToClient(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{transport: mockT, opts: defaultOptions()}
	svc := c.Storage()

	t.Run("Backup", func(t *testing.T) {
		if svc.Backup() != c.Backup() {
			t.Error("Storage().Backup() should delegate to client.Backup()")
		}
	})

	t.Run("ObjectStorage", func(t *testing.T) {
		if svc.ObjectStorage() != c.ObjectStorage() {
			t.Error("Storage().ObjectStorage() should delegate to client.ObjectStorage()")
		}
	})

	t.Run("ObjectStorageAdmin", func(t *testing.T) {
		if svc.ObjectStorageAdmin() != c.ObjectStorageAdmin() {
			t.Error("Storage().ObjectStorageAdmin() should delegate to client.ObjectStorageAdmin()")
		}
	})

	t.Run("ObjectTransfer", func(t *testing.T) {
		if svc.ObjectTransfer() != c.ObjectTransfer() {
			t.Error("Storage().ObjectTransfer() should delegate to client.ObjectTransfer()")
		}
	})

	t.Run("ParameterStore", func(t *testing.T) {
		if svc.ParameterStore() != c.ParameterStore() {
			t.Error("Storage().ParameterStore() should delegate to client.ParameterStore()")
		}
	})
}

// =============================================================================
// Test: AsyncService Delegation
// =============================================================================

func TestAsyncService_DelegatesToClient(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{transport: mockT, opts: defaultOptions()}
	svc := c.Async()

	t.Run("Scheduler", func(t *testing.T) {
		if svc.Scheduler() != c.Scheduler() {
			t.Error("Async().Scheduler() should delegate to client.Scheduler()")
		}
	})

	t.Run("Messaging", func(t *testing.T) {
		if svc.Messaging() != c.Messaging() {
			t.Error("Async().Messaging() should delegate to client.Messaging()")
		}
	})
}

// =============================================================================
// Test: AIService Delegation
// =============================================================================

func TestAIService_DelegatesToClient(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{transport: mockT, opts: defaultOptions()}
	svc := c.AI()

	t.Run("Embedding", func(t *testing.T) {
		if svc.Embedding() != c.Embedding() {
			t.Error("AI().Embedding() should delegate to client.Embedding()")
		}
	})

	t.Run("Ollama", func(t *testing.T) {
		if svc.Ollama() != c.Ollama() {
			t.Error("AI().Ollama() should delegate to client.Ollama()")
		}
	})

	t.Run("RAG", func(t *testing.T) {
		if svc.RAG() != c.RAG() {
			t.Error("AI().RAG() should delegate to client.RAG()")
		}
	})
}

// =============================================================================
// Test: Backward Compatibility - Direct Accessors Still Work
// =============================================================================

func TestBackwardCompatibility_DirectAccessors(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{transport: mockT, opts: defaultOptions()}

	// Verify all direct accessors still work exactly as before
	if c.KV() == nil {
		t.Error("KV() should return non-nil client")
	}
	if c.Batch() == nil {
		t.Error("Batch() should return non-nil client")
	}
	if c.Transaction() == nil {
		t.Error("Transaction() should return non-nil client")
	}
	if c.Query() == nil {
		t.Error("Query() should return non-nil client")
	}
	if c.Cluster() == nil {
		t.Error("Cluster() should return non-nil client")
	}
	if c.Health() == nil {
		t.Error("Health() should return non-nil client")
	}
	if c.auth() == nil {
		t.Error("Auth() should return non-nil client")
	}
	if c.kms() == nil {
		t.Error("KMS() should return non-nil client")
	}
	if c.secrets() == nil {
		t.Error("Secrets() should return non-nil client")
	}
	if c.crypto() == nil {
		t.Error("Crypto() should return non-nil client")
	}
	if c.Vector() == nil {
		t.Error("Vector() should return non-nil client")
	}
	if c.TimeSeries() == nil {
		t.Error("TimeSeries() should return non-nil client")
	}
	if c.Backup() == nil {
		t.Error("Backup() should return non-nil client")
	}
	if c.Identity().IAM() == nil {
		t.Error("Identity().IAM() should return non-nil client")
	}
	if c.Cache() == nil {
		t.Error("Cache() should return non-nil client")
	}
	if c.Operations() == nil {
		t.Error("Operations() should return non-nil client")
	}
	if c.rbac() == nil {
		t.Error("RBAC() should return non-nil client")
	}
	if c.dynamicSecrets() == nil {
		t.Error("DynamicSecrets() should return non-nil client")
	}
	if c.certificate() == nil {
		t.Error("Certificate() should return non-nil client")
	}
	if c.ObjectStorage() == nil {
		t.Error("ObjectStorage() should return non-nil client")
	}
	if c.ObjectStorageAdmin() == nil {
		t.Error("ObjectStorageAdmin() should return non-nil client")
	}
	if c.ObjectTransfer() == nil {
		t.Error("ObjectTransfer() should return non-nil client")
	}
	if c.ParameterStore() == nil {
		t.Error("ParameterStore() should return non-nil client")
	}
	if c.Scheduler() == nil {
		t.Error("Scheduler() should return non-nil client")
	}
	if c.Messaging() == nil {
		t.Error("Messaging() should return non-nil client")
	}
	if c.Embedding() == nil {
		t.Error("Embedding() should return non-nil client")
	}
	if c.Ollama() == nil {
		t.Error("Ollama() should return non-nil client")
	}
	if c.RAG() == nil {
		t.Error("RAG() should return non-nil client")
	}
	if c.Project() == nil {
		t.Error("Project() should return non-nil client")
	}
	// Account() is the single canonical entry point for the pooled provider
	// account surface; a nil here means the accessor never wired a client and
	// every account verb would nil-panic at the call site.
	if c.Account() == nil {
		t.Error("Account() should return non-nil client")
	}
	if c.Account() != c.Account() {
		t.Error("Account() should memoize its client rather than rebuilding it per call")
	}
}

// =============================================================================
// Test: XKMS Sub-Interface Re-exports Exist
// =============================================================================

func TestXKMSReExports_TypeAliasesCompile(t *testing.T) {
	// These are compile-time checks that the type aliases exist and resolve
	// to the correct go-xkms types. If this test compiles, the aliases work.
	var _ XKMSKeyService = (xkmstransport.KeyService)(nil)
	var _ XKMSCryptoService = (xkmstransport.CryptoService)(nil)
	var _ XKMSCertService = (xkmstransport.CertService)(nil)
	var _ XKMSSealService = (xkmstransport.SealService)(nil)
	var _ XKMSBarrierService = (xkmstransport.BarrierService)(nil)
	var _ XKMSPIVService = (xkmstransport.PIVService)(nil)
	var _ XKMSFIDO2Service = (xkmstransport.FIDO2Service)(nil)
	var _ TrustedCAService = (xkmstransport.CAService)(nil)
	var _ XKMSPINService = (xkmstransport.PINService)(nil)
	var _ XKMSUserService = (xkmstransport.UserService)(nil)
	var _ XKMSConnectionService = (xkmstransport.ConnectionService)(nil)
	var _ XKMSBackendService = (xkmstransport.BackendService)(nil)
}

// =============================================================================
// Test: Multiple Sub-Interface Calls Return Same Underlying Clients
// =============================================================================

func TestSubInterface_MultipleCalls_SameUnderlyingClients(t *testing.T) {
	mockT := &mockTransport{}
	c := &client{transport: mockT, opts: defaultOptions()}

	// Call sub-interface accessor, then direct accessor, verify same instance
	coreData := c.CoreData()
	kvFromSub := coreData.KV()
	kvDirect := c.KV()

	if kvFromSub != kvDirect {
		t.Error("sub-interface KV and direct KV should return the same cached instance")
	}

	// Call direct accessor first, then sub-interface, verify same instance
	c2 := &client{transport: mockT, opts: defaultOptions()}
	batchDirect := c2.Batch()
	batchFromSub := c2.CoreData().Batch()

	if batchDirect != batchFromSub {
		t.Error("direct Batch and sub-interface Batch should return the same cached instance")
	}
}
