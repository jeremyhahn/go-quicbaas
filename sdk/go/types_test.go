// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package quicbaas

import (
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/backup"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
)

func TestWithSecretVersion(t *testing.T) {
	// Test that WithSecretVersion returns a function
	opt := WithSecretVersion(5)
	if opt == nil {
		t.Error("WithSecretVersion should return a non-nil function")
	}
}

func TestPutRequest_Fields(t *testing.T) {
	req := &PutRequest{
		EntityType: "users",
		ID:         123,
		Data:       []byte("data"),
	}

	if req.EntityType != "users" {
		t.Error("EntityType not set correctly")
	}
	if req.ID != 123 {
		t.Error("ID not set correctly")
	}
	if string(req.Data) != "data" {
		t.Error("Data not set correctly")
	}
}

func TestBatchOpType_Constants(t *testing.T) {
	if BatchOpPut != 0 {
		t.Errorf("expected BatchOpPut=0, got %d", BatchOpPut)
	}
	if BatchOpDelete != 1 {
		t.Errorf("expected BatchOpDelete=1, got %d", BatchOpDelete)
	}
}

func TestCompareTarget_Constants(t *testing.T) {
	if CompareVersion != 0 {
		t.Errorf("expected CompareVersion=0, got %d", CompareVersion)
	}
	if CompareValue != 1 {
		t.Errorf("expected CompareValue=1, got %d", CompareValue)
	}
	if CompareModRevision != 2 {
		t.Errorf("expected CompareModRevision=2, got %d", CompareModRevision)
	}
	if CompareCreateRevision != 3 {
		t.Errorf("expected CompareCreateRevision=3, got %d", CompareCreateRevision)
	}
}

func TestCompareResult_Constants(t *testing.T) {
	if CompareEqual != 0 {
		t.Errorf("expected CompareEqual=0, got %d", CompareEqual)
	}
	if CompareNotEqual != 1 {
		t.Errorf("expected CompareNotEqual=1, got %d", CompareNotEqual)
	}
	if CompareLess != 2 {
		t.Errorf("expected CompareLess=2, got %d", CompareLess)
	}
	if CompareGreater != 3 {
		t.Errorf("expected CompareGreater=3, got %d", CompareGreater)
	}
}

func TestTxnOpType_Constants(t *testing.T) {
	if TxnOpPut != 0 {
		t.Errorf("expected TxnOpPut=0, got %d", TxnOpPut)
	}
	if TxnOpGet != 1 {
		t.Errorf("expected TxnOpGet=1, got %d", TxnOpGet)
	}
	if TxnOpDelete != 2 {
		t.Errorf("expected TxnOpDelete=2, got %d", TxnOpDelete)
	}
}

func TestQueryOperator_Constants(t *testing.T) {
	if QueryOpEqual != 0 {
		t.Errorf("expected QueryOpEqual=0, got %d", QueryOpEqual)
	}
	if QueryOpNotEqual != 1 {
		t.Errorf("expected QueryOpNotEqual=1, got %d", QueryOpNotEqual)
	}
	if QueryOpLessThan != 2 {
		t.Errorf("expected QueryOpLessThan=2, got %d", QueryOpLessThan)
	}
	if QueryOpLessThanOrEqual != 3 {
		t.Errorf("expected QueryOpLessThanOrEqual=3, got %d", QueryOpLessThanOrEqual)
	}
	if QueryOpGreaterThan != 4 {
		t.Errorf("expected QueryOpGreaterThan=4, got %d", QueryOpGreaterThan)
	}
	if QueryOpGreaterThanEqual != 5 {
		t.Errorf("expected QueryOpGreaterThanEqual=5, got %d", QueryOpGreaterThanEqual)
	}
	if QueryOpPrefix != 6 {
		t.Errorf("expected QueryOpPrefix=6, got %d", QueryOpPrefix)
	}
	if QueryOpRange != 7 {
		t.Errorf("expected QueryOpRange=7, got %d", QueryOpRange)
	}
}

func TestNodeState_Constants(t *testing.T) {
	if NodeStateUnknown != 0 {
		t.Errorf("expected NodeStateUnknown=0, got %d", NodeStateUnknown)
	}
	if NodeStateHealthy != 1 {
		t.Errorf("expected NodeStateHealthy=1, got %d", NodeStateHealthy)
	}
	if NodeStateUnhealthy != 2 {
		t.Errorf("expected NodeStateUnhealthy=2, got %d", NodeStateUnhealthy)
	}
	if NodeStateLeaving != 3 {
		t.Errorf("expected NodeStateLeaving=3, got %d", NodeStateLeaving)
	}
}

func TestKeyAlgorithm_Constants(t *testing.T) {
	if AlgorithmAES256GCM != 0 {
		t.Errorf("expected AlgorithmAES256GCM=0, got %d", AlgorithmAES256GCM)
	}
	if AlgorithmChaCha20Poly1305 != 1 {
		t.Errorf("expected AlgorithmChaCha20Poly1305=1, got %d", AlgorithmChaCha20Poly1305)
	}
	if AlgorithmRSA2048 != 2 {
		t.Errorf("expected AlgorithmRSA2048=2, got %d", AlgorithmRSA2048)
	}
	if AlgorithmRSA4096 != 3 {
		t.Errorf("expected AlgorithmRSA4096=3, got %d", AlgorithmRSA4096)
	}
	if AlgorithmECDSAP256 != 4 {
		t.Errorf("expected AlgorithmECDSAP256=4, got %d", AlgorithmECDSAP256)
	}
	if AlgorithmECDSAP384 != 5 {
		t.Errorf("expected AlgorithmECDSAP384=5, got %d", AlgorithmECDSAP384)
	}
	if AlgorithmED25519 != 6 {
		t.Errorf("expected AlgorithmED25519=6, got %d", AlgorithmED25519)
	}
}

func TestKeyUsage_Constants(t *testing.T) {
	if KeyUsageEncrypt != 0 {
		t.Errorf("expected KeyUsageEncrypt=0, got %d", KeyUsageEncrypt)
	}
	if KeyUsageDecrypt != 1 {
		t.Errorf("expected KeyUsageDecrypt=1, got %d", KeyUsageDecrypt)
	}
	if KeyUsageSign != 2 {
		t.Errorf("expected KeyUsageSign=2, got %d", KeyUsageSign)
	}
	if KeyUsageVerify != 3 {
		t.Errorf("expected KeyUsageVerify=3, got %d", KeyUsageVerify)
	}
	if KeyUsageWrap != 4 {
		t.Errorf("expected KeyUsageWrap=4, got %d", KeyUsageWrap)
	}
	if KeyUsageUnwrap != 5 {
		t.Errorf("expected KeyUsageUnwrap=5, got %d", KeyUsageUnwrap)
	}
}

func TestHashAlgorithm_Constants(t *testing.T) {
	if HashSHA256 != 0 {
		t.Errorf("expected HashSHA256=0, got %d", HashSHA256)
	}
	if HashSHA384 != 1 {
		t.Errorf("expected HashSHA384=1, got %d", HashSHA384)
	}
	if HashSHA512 != 2 {
		t.Errorf("expected HashSHA512=2, got %d", HashSHA512)
	}
}

func TestAggregateFunction_Constants(t *testing.T) {
	if AggregateNone != 0 {
		t.Errorf("expected AggregateNone=0, got %d", AggregateNone)
	}
	if AggregateSum != 1 {
		t.Errorf("expected AggregateSum=1, got %d", AggregateSum)
	}
	if AggregateAvg != 2 {
		t.Errorf("expected AggregateAvg=2, got %d", AggregateAvg)
	}
	if AggregateMin != 3 {
		t.Errorf("expected AggregateMin=3, got %d", AggregateMin)
	}
	if AggregateMax != 4 {
		t.Errorf("expected AggregateMax=4, got %d", AggregateMax)
	}
	if AggregateCount != 5 {
		t.Errorf("expected AggregateCount=5, got %d", AggregateCount)
	}
}

func TestClusterStatus_Fields(t *testing.T) {
	status := &ClusterStatus{
		ClusterID: "cluster-1",
		LeaderID:  "node-1",
		Members: []ClusterMember{
			{ID: "node-1", Name: "primary", Address: "localhost:50051", IsLeader: true, Healthy: true},
		},
		Healthy: true,
	}

	if status.ClusterID != "cluster-1" {
		t.Error("ClusterID not set correctly")
	}
	if len(status.Members) != 1 {
		t.Error("Members not set correctly")
	}
	if !status.Healthy {
		t.Error("Healthy not set correctly")
	}
}

func TestSealStatus_Fields(t *testing.T) {
	status := &SealStatus{
		Sealed:         true,
		Initialized:    true,
		Progress:       2,
		Required:       3,
		ClusterName:    "test-cluster",
		ClusterVersion: "1.0.0",
	}

	if !status.Sealed {
		t.Error("Sealed not set correctly")
	}
	if !status.Initialized {
		t.Error("Initialized not set correctly")
	}
	if status.Progress != 2 {
		t.Error("Progress not set correctly")
	}
	if status.Required != 3 {
		t.Error("Required not set correctly")
	}
}

func TestKeyInfo_Fields(t *testing.T) {
	now := time.Now()
	info := &KeyInfo{
		KeyID:     "key-123",
		Name:      "my-key",
		Algorithm: AlgorithmAES256GCM,
		Version:   1,
		Usage:     []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
		CreatedAt: now,
		RotatedAt: now,
	}

	if info.KeyID != "key-123" {
		t.Error("KeyID not set correctly")
	}
	if info.Name != "my-key" {
		t.Error("Name not set correctly")
	}
	if info.Algorithm != AlgorithmAES256GCM {
		t.Error("Algorithm not set correctly")
	}
	if len(info.Usage) != 2 {
		t.Error("Usage not set correctly")
	}
}

func TestSecret_Fields(t *testing.T) {
	now := time.Now()
	secret := &Secret{
		Path:      "secret/data/myapp",
		Data:      map[string]interface{}{"key": "value"},
		Metadata:  map[string]string{"owner": "test"},
		Version:   1,
		CreatedAt: now,
	}

	if secret.Path != "secret/data/myapp" {
		t.Error("Path not set correctly")
	}
	if secret.Data["key"] != "value" {
		t.Error("Data not set correctly")
	}
	if secret.Metadata["owner"] != "test" {
		t.Error("Metadata not set correctly")
	}
}

func TestVectorSearchResult_Fields(t *testing.T) {
	result := &VectorSearchResult{
		Vector: &vector.VectorEntity{
			ID:       "vec-123",
			Vector:   []float32{0.1, 0.2, 0.3},
			Metadata: map[string]string{"category": "test"},
		},
		Score:    0.95,
		Distance: 0.05,
		Rank:     1,
	}

	if result.Vector.ID != "vec-123" {
		t.Error("ID not set correctly")
	}
	if result.Score != 0.95 {
		t.Error("Score not set correctly")
	}
	if len(result.Vector.Vector) != 3 {
		t.Error("Vector not set correctly")
	}
	if result.Distance != 0.05 {
		t.Error("Distance not set correctly")
	}
	if result.Rank != 1 {
		t.Error("Rank not set correctly")
	}
}

func TestTimeSeriesPoint_Fields(t *testing.T) {
	now := time.Now()
	point := &TimeSeriesPoint{
		Timestamp: now,
		Value:     42.5,
	}

	if !point.Timestamp.Equal(now) {
		t.Error("Timestamp not set correctly")
	}
	if point.Value != 42.5 {
		t.Error("Value not set correctly")
	}
}

func TestBackupInfo_Fields(t *testing.T) {
	now := time.Now()
	info := &backup.BackupInfo{
		ID:        "backup-123",
		Size:      1024 * 1024,
		CreatedAt: now,
		Encrypted: true,
	}

	if info.ID != "backup-123" {
		t.Error("ID not set correctly")
	}
	if info.Size != 1024*1024 {
		t.Error("Size not set correctly")
	}
	if !info.Encrypted {
		t.Error("Encrypted not set correctly")
	}
}
