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

//go:build integration

package seal

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// Seal state manager error types

// SealStateError represents an error during seal state management.
type SealStateError struct {
	Op  string
	Err error
}

func (e *SealStateError) Error() string {
	if e.Err != nil {
		return "seal state " + e.Op + ": " + e.Err.Error()
	}
	return "seal state " + e.Op
}

func (e *SealStateError) Unwrap() error {
	return e.Err
}

// RekeyError represents an error from the rekey operation.
type RekeyError struct {
	Message string
}

func (e *RekeyError) Error() string {
	return "rekey failed: " + e.Message
}

// UnsealError represents an error from the unseal operation.
type UnsealError struct {
	Message string
}

func (e *UnsealError) Error() string {
	return "unseal failed: " + e.Message
}

// SealError represents an error from the seal operation.
type SealError struct {
	Message string
}

func (e *SealError) Error() string {
	return "seal failed: " + e.Message
}

// Sentinel errors for common conditions
var (
	// ErrNotInitialized indicates the seal is not yet initialized.
	ErrNotInitialized = &NotInitializedError{}
	// ErrSealedNoShares indicates the system is sealed but no shares are available.
	ErrSealedNoShares = &SealedNoSharesError{}
	// ErrUnsealFailed indicates the unseal operation failed.
	ErrUnsealFailed = &UnsealFailedError{}
)

// NotInitializedError indicates the seal is not initialized.
type NotInitializedError struct{}

func (e *NotInitializedError) Error() string {
	return "seal not initialized"
}

// SealedNoSharesError indicates the system is sealed with no available shares.
type SealedNoSharesError struct{}

func (e *SealedNoSharesError) Error() string {
	return "system is sealed and no shares are available"
}

// UnsealFailedError indicates the unseal operation failed after providing shares.
type UnsealFailedError struct{}

func (e *UnsealFailedError) Error() string {
	return "unseal failed after providing threshold shares"
}

// sealStateManager manages seal test state including shares, status, and persistence.
// It provides thread-safe access to seal state and share acquisition via rekey.
type sealStateManager struct {
	mu          sync.RWMutex
	initialized bool
	sealed      bool
	threshold   int32
	totalShares int32
	version     uint64
	shares      [][]byte
	shareFile   string
	endpoint    string
}

// shareFileData represents the JSON structure for persisted shares.
type shareFileData struct {
	Threshold   int32    `json:"threshold"`
	TotalShares int32    `json:"total_shares"`
	Version     uint64   `json:"version"`
	Shares      []string `json:"shares"`
	Timestamp   string   `json:"timestamp"`
	Endpoint    string   `json:"endpoint"`
}

// globalStateManager is the package-level singleton for seal state management.
var globalStateManager *sealStateManager

// initializeSealStateManager creates and initializes the global state manager.
// It queries the server for current seal status and attempts to load any
// persisted shares from previous test runs. If shares don't match the current
// seal version, they are cleared. If the system is unsealed and no shares are
// available, it proactively acquires them via rekey.
func initializeSealStateManager(ctx context.Context, endpoint string) (*sealStateManager, error) {
	shareFile := generateShareFilePath(endpoint)
	mgr := &sealStateManager{
		endpoint:  endpoint,
		shareFile: shareFile,
	}

	log.Printf("initializeSealStateManager: starting, endpoint=%s, shareFile=%s", endpoint, shareFile)

	// Try to load existing shares from file
	if err := mgr.LoadShares(); err != nil {
		log.Printf("initializeSealStateManager: LoadShares error (may be normal): %v", err)
	} else {
		log.Printf("initializeSealStateManager: LoadShares success, hasShares=%v, version=%d", len(mgr.shares) > 0, mgr.version)
	}

	// Save the stored version before RefreshStatus overwrites it
	mgr.mu.RLock()
	storedVersion := mgr.version
	hasStoredShares := len(mgr.shares) > 0
	mgr.mu.RUnlock()

	log.Printf("initializeSealStateManager: before RefreshStatus, storedVersion=%d, hasStoredShares=%v", storedVersion, hasStoredShares)

	// Query current server status
	if err := mgr.RefreshStatus(ctx); err != nil {
		return nil, err
	}

	// Clear stale shares if stored version doesn't match server version
	mgr.mu.RLock()
	serverVersion := mgr.version
	serverInit := mgr.initialized
	serverSealed := mgr.sealed
	mgr.mu.RUnlock()

	log.Printf("initializeSealStateManager: after RefreshStatus, serverVersion=%d, serverInit=%v, serverSealed=%v",
		serverVersion, serverInit, serverSealed)

	if hasStoredShares && storedVersion != serverVersion {
		log.Printf("initializeSealStateManager: CLEARING stale shares (storedVersion=%d != serverVersion=%d)",
			storedVersion, serverVersion)
		mgr.ClearStaleSharesInternal()
	}

	// If system is initialized, unsealed, and we don't have shares, acquire them proactively
	// This ensures shares are available for all tests regardless of test order
	mgr.mu.RLock()
	initialized := mgr.initialized
	sealed := mgr.sealed
	hasShares := len(mgr.shares) > 0
	mgr.mu.RUnlock()

	log.Printf("initializeSealStateManager: checking proactive acquire, init=%v, sealed=%v, hasShares=%v",
		initialized, sealed, hasShares)

	if initialized && !sealed && !hasShares {
		log.Printf("initializeSealStateManager: attempting proactive share acquisition via rekey")
		// Create a gRPC client to acquire shares
		conn, err := helpers.SetupGRPCConnectionToEndpoint(nil, ctx, endpoint)
		if err == nil {
			defer conn.Close()
			client := pb.NewQuicBaaSClient(conn)

			// Try to acquire shares via rekey
			if _, err := mgr.AcquireShares(ctx, client); err != nil {
				log.Printf("initializeSealStateManager: proactive acquire failed: %v", err)
			} else {
				log.Printf("initializeSealStateManager: proactive acquire success, hasShares=%v", mgr.HasShares())
			}
		}
	}

	globalStateManager = mgr
	log.Printf("initializeSealStateManager: complete, globalStateManager set")
	return mgr, nil
}

// getSealStateManager returns the initialized singleton state manager.
// Returns nil if not initialized.
func getSealStateManager() *sealStateManager {
	return globalStateManager
}

// generateShareFilePath creates a deterministic path for share persistence.
// Uses /test-results if available (Docker integration tests) for persistence
// across container restarts, otherwise falls back to temp directory.
func generateShareFilePath(endpoint string) string {
	hash := sha256.Sum256([]byte(endpoint))
	hashStr := hex.EncodeToString(hash[:8])
	filename := "quicbaas-seal-test-shares-" + hashStr + ".json"

	// In Docker integration tests, use /test-results for persistence across
	// container restarts. This ensures shares survive when server volumes
	// persist but test container is recreated.
	testResultsDir := "/test-results"
	if info, err := os.Stat(testResultsDir); err == nil && info.IsDir() {
		return filepath.Join(testResultsDir, filename)
	}

	// Fall back to temp directory for local development/testing
	return filepath.Join(os.TempDir(), filename)
}

// RefreshStatus queries the server and updates the internal state.
func (m *sealStateManager) RefreshStatus(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conn, err := helpers.SetupGRPCConnectionToEndpoint(nil, ctx, m.endpoint)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pb.NewQuicBaaSClient(conn)
	resp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
	if err != nil {
		return err
	}

	m.initialized = resp.Initialized
	m.sealed = resp.Sealed
	m.threshold = resp.Threshold
	m.totalShares = resp.TotalShares
	m.version = resp.Version

	return nil
}

// GetShares returns the currently available shares (thread-safe).
func (m *sealStateManager) GetShares() [][]byte {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.shares) == 0 {
		log.Printf("GetShares: no shares available (version=%d)", m.version)
		return nil
	}

	// Log share lengths for debugging
	shareLens := make([]int, len(m.shares))
	for i, s := range m.shares {
		shareLens[i] = len(s)
	}
	log.Printf("GetShares: returning %d shares (version=%d), lengths=%v", len(m.shares), m.version, shareLens)

	// Return a copy to prevent external mutation
	shares := make([][]byte, len(m.shares))
	for i, s := range m.shares {
		shares[i] = make([]byte, len(s))
		copy(shares[i], s)
	}
	return shares
}

// HasShares returns true if shares are available.
func (m *sealStateManager) HasShares() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.shares) > 0
}

// GetStatus returns the current seal status (thread-safe).
func (m *sealStateManager) GetStatus() (initialized, sealed bool, threshold, totalShares int32, version uint64) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.initialized, m.sealed, m.threshold, m.totalShares, m.version
}

// IsInitialized returns true if the seal is initialized.
func (m *sealStateManager) IsInitialized() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.initialized
}

// IsSealed returns true if the system is currently sealed.
func (m *sealStateManager) IsSealed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sealed
}

// SaveShares persists shares to disk with restrictive permissions.
func (m *sealStateManager) SaveShares(shares [][]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.shares = shares

	// Debug logging to track share operations - including lengths
	shareLens := make([]int, len(shares))
	for i, s := range shares {
		shareLens[i] = len(s)
	}
	log.Printf("SaveShares: saving %d shares, version=%d, file=%s, lengths=%v", len(shares), m.version, m.shareFile, shareLens)

	// Encode shares to base64
	encodedShares := make([]string, len(shares))
	for i, s := range shares {
		encodedShares[i] = base64.StdEncoding.EncodeToString(s)
	}

	data := shareFileData{
		Threshold:   m.threshold,
		TotalShares: m.totalShares,
		Version:     m.version,
		Shares:      encodedShares,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Endpoint:    m.endpoint,
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}

	// Write with restrictive permissions (owner read/write only)
	return os.WriteFile(m.shareFile, jsonData, 0600)
}

// LoadShares reads shares from disk if available.
// Returns error if shares cannot be loaded or are incompatible.
func (m *sealStateManager) LoadShares() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	jsonData, err := os.ReadFile(m.shareFile)
	if err != nil {
		return err
	}

	var data shareFileData
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return err
	}

	// Verify endpoint matches
	if data.Endpoint != m.endpoint {
		// Different endpoint - ignore the file
		return nil
	}

	// Decode shares from base64
	shares := make([][]byte, len(data.Shares))
	for i, s := range data.Shares {
		decoded, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return err
		}
		shares[i] = decoded
	}

	m.shares = shares
	m.threshold = data.Threshold
	m.totalShares = data.TotalShares
	m.version = data.Version

	return nil
}

// ClearStaleShares removes shares if they don't match the given version.
// This should be called after RefreshStatus to validate stored shares.
func (m *sealStateManager) ClearStaleShares(serverVersion uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.shares) > 0 && m.version != serverVersion {
		// Shares are stale - clear them
		log.Printf("ClearStaleShares: clearing %d stale shares (m.version=%d, serverVersion=%d)",
			len(m.shares), m.version, serverVersion)
		m.shares = nil
		// Also remove the share file
		os.Remove(m.shareFile)
		return true
	}
	return false
}

// ClearStaleSharesInternal unconditionally clears shares and the share file.
// Used when the caller has already determined shares are stale.
func (m *sealStateManager) ClearStaleSharesInternal() {
	m.mu.Lock()
	defer m.mu.Unlock()

	log.Printf("ClearStaleSharesInternal: clearing %d shares (version=%d)", len(m.shares), m.version)
	m.shares = nil
	os.Remove(m.shareFile)
}

// AcquireShares obtains shares for testing.
// If shares are already available and match the current seal version, returns them.
// If system is unsealed and initialized, uses SealRekey to get new shares.
// Returns error if system is sealed and no shares are available.
func (m *sealStateManager) AcquireShares(ctx context.Context, client pb.QuicBaaSClient) ([][]byte, error) {
	log.Printf("AcquireShares: starting")

	// Refresh status to get current state first
	if err := m.RefreshStatus(ctx); err != nil {
		return nil, &SealStateError{Op: "refresh_status", Err: err}
	}

	m.mu.Lock()
	initialized := m.initialized
	sealed := m.sealed
	threshold := m.threshold
	total := m.totalShares
	serverVersion := m.version
	storedVersion := m.version
	hasShares := len(m.shares) > 0
	m.mu.Unlock()

	log.Printf("AcquireShares: init=%v, sealed=%v, threshold=%d, total=%d, version=%d, hasShares=%v",
		initialized, sealed, threshold, total, serverVersion, hasShares)

	// Check if we already have shares that match the current version
	if hasShares {
		// Get the version from the server to compare
		resp, err := client.SealStatus(ctx, &pb.SealStatusRequest{})
		if err == nil && resp.Version == storedVersion {
			// Shares match current version, return them
			log.Printf("AcquireShares: returning existing shares (version match: %d)", storedVersion)
			return m.GetShares(), nil
		}
		// Version mismatch - shares are stale, need to acquire new ones
		if resp != nil {
			log.Printf("AcquireShares: version mismatch (stored=%d, server=%d), will rekey", storedVersion, resp.Version)
			serverVersion = resp.Version
		}
	}

	if !initialized {
		log.Printf("AcquireShares: not initialized, returning error")
		return nil, &SealStateError{Op: "acquire_shares", Err: ErrNotInitialized}
	}

	if sealed {
		// If sealed and we have shares (even stale ones), try to use them
		// They might still work if the seal was just sealed without rekey
		if hasShares {
			log.Printf("AcquireShares: sealed but have shares, returning them")
			return m.GetShares(), nil
		}
		log.Printf("AcquireShares: sealed with no shares, returning error")
		return nil, &SealStateError{Op: "acquire_shares", Err: ErrSealedNoShares}
	}

	// System is unsealed and initialized - use rekey to get fresh shares
	// This is the key innovation: rekey with same config gives us new valid shares
	log.Printf("AcquireShares: performing rekey with threshold=%d, total=%d", threshold, total)
	rekeyResp, err := client.SealRekey(ctx, &pb.SealRekeyRequest{
		NewThreshold: threshold,
		NewTotal:     total,
	})
	if err != nil {
		log.Printf("AcquireShares: rekey error: %v", err)
		return nil, &SealStateError{Op: "rekey", Err: err}
	}

	if rekeyResp.Error != "" {
		log.Printf("AcquireShares: rekey response error: %s", rekeyResp.Error)
		return nil, &SealStateError{Op: "rekey", Err: &RekeyError{Message: rekeyResp.Error}}
	}

	// Store and persist shares
	log.Printf("AcquireShares: rekey success, got %d shares, new version=%d", len(rekeyResp.Shares), rekeyResp.Version)
	m.mu.Lock()
	m.shares = rekeyResp.Shares
	m.version = rekeyResp.Version
	m.threshold = threshold
	m.totalShares = total
	_ = serverVersion // Acknowledge variable (used in version check above)
	m.mu.Unlock()

	if err := m.SaveShares(rekeyResp.Shares); err != nil {
		// Log but don't fail - shares are still in memory
		log.Printf("AcquireShares: SaveShares error (non-fatal): %v", err)
	}

	return rekeyResp.Shares, nil
}

// ResetAccumulator forces a seal operation on the server to reset the
// server-side share accumulator. This prevents "duplicate share submitted"
// errors caused by stale shares from previous test runs lingering in the
// accumulator. The system remains sealed after this call.
func (m *sealStateManager) ResetAccumulator(ctx context.Context, client pb.QuicBaaSClient) {
	log.Printf("ResetAccumulator: forcing seal to clear server-side accumulator")
	_, _ = client.Seal(ctx, &pb.SealRequest{Force: true})
}

// EnsureUnsealed transitions the system to an unsealed state if currently sealed.
// Uses available shares to unseal if needed. If shares are stale or invalid,
// they are cleared and the error is returned for the test to handle.
//
// To prevent cross-test state pollution, EnsureUnsealed always forces a seal
// before submitting shares. This guarantees the server-side share accumulator
// is empty, avoiding "duplicate share submitted" errors from previous tests.
func (m *sealStateManager) EnsureUnsealed(ctx context.Context, client pb.QuicBaaSClient) error {
	log.Printf("EnsureUnsealed: starting")

	// Refresh status first
	if err := m.RefreshStatus(ctx); err != nil {
		return &SealStateError{Op: "refresh_status", Err: err}
	}

	m.mu.RLock()
	sealed := m.sealed
	initialized := m.initialized
	threshold := m.threshold
	serverVersion := m.version
	m.mu.RUnlock()

	log.Printf("EnsureUnsealed: sealed=%v, init=%v, threshold=%d, serverVersion=%d", sealed, initialized, threshold, serverVersion)

	if !initialized {
		return &SealStateError{Op: "ensure_unsealed", Err: ErrNotInitialized}
	}

	if !sealed {
		// Already unsealed
		log.Printf("EnsureUnsealed: already unsealed, returning nil")
		return nil
	}

	// Clear stale shares before attempting unseal
	if m.ClearStaleShares(serverVersion) {
		// Shares were stale and cleared - we have no shares now
		log.Printf("EnsureUnsealed: stale shares cleared, returning error")
		return &SealStateError{Op: "ensure_unsealed", Err: ErrSealedNoShares}
	}

	// Always force a seal to reset the server-side share accumulator.
	// This prevents "duplicate share submitted" errors caused by shares
	// from a previous test lingering in the accumulator. The seal operation
	// unconditionally resets the accumulator even if the system is already
	// sealed, so this is safe and idempotent.
	log.Printf("EnsureUnsealed: forcing seal to reset server-side accumulator")
	m.ResetAccumulator(ctx, client)

	// Need to unseal - get shares
	shares := m.GetShares()
	if len(shares) == 0 {
		log.Printf("EnsureUnsealed: no shares available after GetShares")
		return &SealStateError{Op: "ensure_unsealed", Err: ErrSealedNoShares}
	}

	log.Printf("EnsureUnsealed: attempting unseal with %d shares, threshold=%d", len(shares), threshold)

	// Provide threshold shares to unseal
	for i := 0; i < int(threshold) && i < len(shares); i++ {
		log.Printf("EnsureUnsealed: providing share %d/%d", i+1, threshold)
		resp, err := client.Unseal(ctx, &pb.UnsealRequest{Share: shares[i]})
		if err != nil {
			log.Printf("EnsureUnsealed: unseal RPC error: %v", err)
			return &SealStateError{Op: "unseal", Err: err}
		}
		if resp.Error != "" {
			log.Printf("EnsureUnsealed: unseal response error: %s", resp.Error)
			// If error indicates invalid shares, clear them
			if containsAny(resp.Error, "inconsistent", "invalid", "duplicate", "no shamir") {
				log.Printf("EnsureUnsealed: CLEARING shares due to error: %s", resp.Error)
				m.mu.Lock()
				m.shares = nil
				os.Remove(m.shareFile)
				m.mu.Unlock()
			}
			return &SealStateError{Op: "unseal", Err: &UnsealError{Message: resp.Error}}
		}
		log.Printf("EnsureUnsealed: share %d accepted, sealed=%v, shares_provided=%d", i+1, resp.Sealed, resp.SharesProvided)
		if !resp.Sealed {
			// Successfully unsealed
			log.Printf("EnsureUnsealed: successfully unsealed!")
			m.mu.Lock()
			m.sealed = false
			m.mu.Unlock()
			return nil
		}
	}

	// Verify unsealed
	if err := m.RefreshStatus(ctx); err != nil {
		return &SealStateError{Op: "verify_unseal", Err: err}
	}

	m.mu.RLock()
	stillSealed := m.sealed
	m.mu.RUnlock()

	if stillSealed {
		return &SealStateError{Op: "ensure_unsealed", Err: ErrUnsealFailed}
	}

	return nil
}

// containsAny checks if str contains any of the substrings.
func containsAny(str string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(str) >= len(sub) {
			for i := 0; i <= len(str)-len(sub); i++ {
				if str[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// EnsureSealed transitions the system to a sealed state if currently unsealed.
// Before sealing, it acquires fresh shares via rekey to ensure valid shares are
// available for subsequent unseal operations.
func (m *sealStateManager) EnsureSealed(ctx context.Context, client pb.QuicBaaSClient) error {
	// Refresh status first
	if err := m.RefreshStatus(ctx); err != nil {
		return &SealStateError{Op: "refresh_status", Err: err}
	}

	m.mu.RLock()
	sealed := m.sealed
	initialized := m.initialized
	m.mu.RUnlock()

	if !initialized {
		return &SealStateError{Op: "ensure_sealed", Err: ErrNotInitialized}
	}

	if sealed {
		// Already sealed, but still force-seal to reset the accumulator.
		// This prevents stale shares from a previous test from causing
		// "duplicate share submitted" errors in subsequent unseal attempts.
		log.Printf("EnsureSealed: already sealed, resetting accumulator")
		m.ResetAccumulator(ctx, client)
		return nil
	}

	// Before sealing, acquire fresh shares via rekey to ensure we have valid shares
	// for subsequent unseal operations. This is critical because sealing invalidates
	// the ability to acquire new shares (rekey requires unsealed state).
	if _, err := m.AcquireShares(ctx, client); err != nil {
		// Log but continue - we may already have valid shares from a previous operation
		log.Printf("EnsureSealed: AcquireShares failed (non-fatal): %v", err)
	}

	// Seal the system
	resp, err := client.Seal(ctx, &pb.SealRequest{Force: false})
	if err != nil {
		return &SealStateError{Op: "seal", Err: err}
	}
	if resp.Error != "" {
		return &SealStateError{Op: "seal", Err: &SealError{Message: resp.Error}}
	}

	m.mu.Lock()
	m.sealed = true
	m.mu.Unlock()

	return nil
}

// SetShares updates the in-memory shares without persisting.
// Used by tests that perform their own init/rekey operations.
func (m *sealStateManager) SetShares(shares [][]byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shares = shares
}

// UpdateConfig updates the threshold and total shares configuration.
func (m *sealStateManager) UpdateConfig(threshold, totalShares int32, version uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.threshold = threshold
	m.totalShares = totalShares
	m.version = version
}

// Cleanup removes the share file and resets state.
func (m *sealStateManager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove share file
	os.Remove(m.shareFile)

	// Clear in-memory state
	m.shares = nil
}

// GetShareFilePath returns the path to the share file (for debugging).
func (m *sealStateManager) GetShareFilePath() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.shareFile
}

// GetVersion returns the current seal version (thread-safe).
func (m *sealStateManager) GetVersion() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.version
}

// createConnection creates a gRPC connection to the configured endpoint.
func (m *sealStateManager) createConnection(ctx context.Context) (*grpc.ClientConn, error) {
	return helpers.SetupGRPCConnectionToEndpoint(nil, ctx, m.endpoint)
}
