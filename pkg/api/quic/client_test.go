//go:build quic

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

package quic

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock Stream Infrastructure
// =============================================================================

// MockStream implements io.ReadWriteCloser for testing QUIC streams.
type MockStream struct {
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
	closed   bool
	mu       sync.Mutex
}

// NewMockStream creates a new mock stream with optional pre-filled read data.
func NewMockStream(readData []byte) *MockStream {
	return &MockStream{
		readBuf:  bytes.NewBuffer(readData),
		writeBuf: &bytes.Buffer{},
	}
}

func (m *MockStream) Read(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, io.EOF
	}
	return m.readBuf.Read(p)
}

func (m *MockStream) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, io.ErrClosedPipe
	}
	return m.writeBuf.Write(p)
}

func (m *MockStream) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// WrittenData returns the data written to the stream.
func (m *MockStream) WrittenData() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeBuf.Bytes()
}

// createClientTestLogger creates a logger for client testing that discards output
func createClientTestLogger() *logger.Logger {
	cfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "quic-client-test",
	}
	log, _ := logger.New(cfg)
	return log
}

// =============================================================================
// Mock Stream Tests
// =============================================================================

func TestMockStream_ReadWrite(t *testing.T) {
	data := []byte("test data")
	stream := NewMockStream(data)

	buf := make([]byte, len(data))
	n, err := stream.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("expected to read %d bytes, got %d", len(data), n)
	}
	if string(buf) != string(data) {
		t.Errorf("expected '%s', got '%s'", string(data), string(buf))
	}

	writeData := []byte("write test")
	n, err = stream.Write(writeData)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(writeData) {
		t.Errorf("expected to write %d bytes, got %d", len(writeData), n)
	}

	written := stream.WrittenData()
	if string(written) != string(writeData) {
		t.Errorf("expected written data '%s', got '%s'", string(writeData), string(written))
	}
}

func TestMockStream_Close(t *testing.T) {
	stream := NewMockStream(nil)

	err := stream.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	buf := make([]byte, 10)
	_, err = stream.Read(buf)
	if err != io.EOF {
		t.Errorf("expected EOF after close, got %v", err)
	}

	_, err = stream.Write([]byte("test"))
	if err != io.ErrClosedPipe {
		t.Errorf("expected ErrClosedPipe after close, got %v", err)
	}
}

// =============================================================================
// Client Options Tests
// =============================================================================

func TestClientOption_WithTimeout(t *testing.T) {
	cfg := &clientConfig{}
	opt := WithTimeout(30 * time.Second)
	opt(cfg)
	if cfg.timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", cfg.timeout)
	}
}

func TestClientOption_WithMaxIdleTimeout(t *testing.T) {
	cfg := &clientConfig{}
	opt := WithMaxIdleTimeout(60 * time.Second)
	opt(cfg)
	if cfg.maxIdleTimeout != 60*time.Second {
		t.Errorf("expected maxIdleTimeout 60s, got %v", cfg.maxIdleTimeout)
	}
}

// =============================================================================
// Status Code to Error Mapping Tests
// =============================================================================

func TestClientStatusToError_NotFound(t *testing.T) {
	err := StatusToError(StatusNotFound, "key not found")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClientStatusToError_Unauthorized(t *testing.T) {
	err := StatusToError(StatusUnauthorized, "unauthorized")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClientStatusToError_PermissionDenied(t *testing.T) {
	err := StatusToError(StatusPermissionDenied, "forbidden")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClientStatusToError_AlreadyExists(t *testing.T) {
	err := StatusToError(StatusAlreadyExists, "already exists")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClientStatusToError_Timeout(t *testing.T) {
	err := StatusToError(StatusTimeout, "timeout")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClientStatusToError_ResourceExhausted(t *testing.T) {
	err := StatusToError(StatusResourceExhausted, "resource exhausted")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClientStatusToError_Unavailable(t *testing.T) {
	err := StatusToError(StatusUnavailable, "unavailable")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClientStatusToError_InvalidRequest(t *testing.T) {
	err := StatusToError(StatusInvalidRequest, "invalid request")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClientStatusToError_InvalidArgument(t *testing.T) {
	err := StatusToError(StatusInvalidArgument, "invalid argument")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClientStatusToError_InternalError(t *testing.T) {
	err := StatusToError(StatusInternalError, "internal error")
	if err == nil {
		t.Fatal("expected error")
	}
}

// =============================================================================
// NewClient Error Tests
// =============================================================================

func TestNewClient_EmptyAddress(t *testing.T) {
	_, err := NewClient("")
	if err == nil {
		t.Fatal("expected error for empty address")
	}
}

func TestNewClient_ConnectionFailure(t *testing.T) {
	// Connect to a non-existent server with short timeout
	_, err := NewClient("invalid-host-that-does-not-exist:9999", WithTimeout(1*time.Millisecond))
	if err == nil {
		t.Fatal("expected error for connection failure")
	}
}

// =============================================================================
// Opcode Constants Tests
// =============================================================================

func TestClientOpcodeConstants(t *testing.T) {
	// Verify opcodes are unique
	opcodes := map[byte]string{
		OpPut:               "OpPut",
		OpGet:               "OpGet",
		OpDelete:            "OpDelete",
		OpList:              "OpList",
		OpKMSGenerateKey:    "OpKMSGenerateKey",
		OpKMSImportKey:      "OpKMSImportKey",
		OpKMSGetKey:         "OpKMSGetKey",
		OpKMSListKeys:       "OpKMSListKeys",
		OpKMSRotateKey:      "OpKMSRotateKey",
		OpKMSDeleteKey:      "OpKMSDeleteKey",
		OpKMSEncrypt:        "OpKMSEncrypt",
		OpKMSDecrypt:        "OpKMSDecrypt",
		OpKMSKeyVersions:    "OpKMSKeyVersions",
		OpKMSKeyStatus:      "OpKMSKeyStatus",
		OpCryptoDataKey:     "OpCryptoDataKey",
		OpBarrier:           "OpBarrier",
		OpCeremony:          "OpCeremony",
		OpSecretsStore:      "OpSecretsStore",
		OpSecretsGet:        "OpSecretsGet",
		OpSecretsList:       "OpSecretsList",
		OpSecretsDelete:     "OpSecretsDelete",
		OpClusterStatus:     "OpClusterStatus",
		OpGetLeader:         "OpGetLeader",
		OpAddNode:           "OpAddNode",
		OpRemoveNode:        "OpRemoveNode",
		OpCreateShard:       "OpCreateShard",
		OpRebalanceCluster:  "OpRebalanceCluster",
		OpScheduler:         "OpScheduler",
		OpKMSDEKCreate:      "OpKMSDEKCreate",
		OpKMSDEKGet:         "OpKMSDEKGet",
		OpKMSDEKList:        "OpKMSDEKList",
		OpKMSDEKRotate:      "OpKMSDEKRotate",
		OpKMSDEKDelete:      "OpKMSDEKDelete",
		OpKMSDEKStatus:      "OpKMSDEKStatus",
		OpKMSDEKSetDefault:  "OpKMSDEKSetDefault",
		OpKMSDEKSetSafety:   "OpKMSDEKSetSafety",
		OpKMSDEKClearSafety: "OpKMSDEKClearSafety",
		OpKMSExportKey:      "OpKMSExportKey",
		OpKMSUnwrapKey:      "OpKMSUnwrapKey",
		OpKMSSetKeySafety:   "OpKMSSetKeySafety",
		OpKMSClearKeySafety: "OpKMSClearKeySafety",
		OpKMSSafetyCheck:    "OpKMSSafetyCheck",
		OpKMSSafetyReport:   "OpKMSSafetyReport",
		OpKMSBackendStatus:  "OpKMSBackendStatus",
		OpKMSBackendTest:    "OpKMSBackendTest",
	}

	// Build reverse map to find duplicates
	reverseMap := make(map[byte][]string)
	for opcode, name := range opcodes {
		reverseMap[opcode] = append(reverseMap[opcode], name)
	}

	for opcode, names := range reverseMap {
		if len(names) > 1 {
			t.Errorf("duplicate opcode %d: %v", opcode, names)
		}
	}
}

// =============================================================================
// Status Constants Tests
// =============================================================================

func TestClientStatusConstants(t *testing.T) {
	statuses := map[byte]string{
		StatusOK:                "StatusOK",
		StatusNotFound:          "StatusNotFound",
		StatusAlreadyExists:     "StatusAlreadyExists",
		StatusUnauthorized:      "StatusUnauthorized",
		StatusPermissionDenied:  "StatusPermissionDenied",
		StatusInvalidRequest:    "StatusInvalidRequest",
		StatusInvalidArgument:   "StatusInvalidArgument",
		StatusInternalError:     "StatusInternalError",
		StatusTimeout:           "StatusTimeout",
		StatusResourceExhausted: "StatusResourceExhausted",
		StatusUnavailable:       "StatusUnavailable",
	}

	// Verify StatusOK is 0
	if StatusOK != 0 {
		t.Error("StatusOK should be 0")
	}

	// Verify all statuses are unique
	reverseMap := make(map[byte][]string)
	for status, name := range statuses {
		reverseMap[status] = append(reverseMap[status], name)
	}

	for status, names := range reverseMap {
		if len(names) > 1 {
			t.Errorf("duplicate status %d: %v", status, names)
		}
	}
}

// =============================================================================
// Scheduler Sub-Opcode Tests
// =============================================================================

func TestClientSchedulerSubOpcodes(t *testing.T) {
	subOpcodes := map[byte]string{
		SubOpSchedulerCreateJob:     "SubOpSchedulerCreateJob",
		SubOpSchedulerGetJob:        "SubOpSchedulerGetJob",
		SubOpSchedulerUpdateJob:     "SubOpSchedulerUpdateJob",
		SubOpSchedulerDeleteJob:     "SubOpSchedulerDeleteJob",
		SubOpSchedulerListJobs:      "SubOpSchedulerListJobs",
		SubOpSchedulerTriggerJob:    "SubOpSchedulerTriggerJob",
		SubOpSchedulerPauseJob:      "SubOpSchedulerPauseJob",
		SubOpSchedulerResumeJob:     "SubOpSchedulerResumeJob",
		SubOpSchedulerCancelJob:     "SubOpSchedulerCancelJob",
		SubOpSchedulerGetJobHistory: "SubOpSchedulerGetJobHistory",
		SubOpSchedulerGetStatus:     "SubOpSchedulerGetStatus",
	}

	// Verify all sub-opcodes are unique
	reverseMap := make(map[byte][]string)
	for subOp, name := range subOpcodes {
		reverseMap[subOp] = append(reverseMap[subOp], name)
	}

	for subOp, names := range reverseMap {
		if len(names) > 1 {
			t.Errorf("duplicate scheduler sub-opcode %d: %v", subOp, names)
		}
	}
}

// =============================================================================
// Client Close Tests
// =============================================================================

func TestClientCloseNilConnection(t *testing.T) {
	// Create a client with nil connection (simulating failed connection)
	// Must properly initialize cancelFunc and logger to avoid panic
	ctx, cancel := context.WithCancel(context.Background())
	log := createClientTestLogger()

	client := &Client{
		addr:       "test:8443",
		connected:  false,
		closed:     false,
		ctx:        ctx,
		cancelFunc: cancel,
		logger:     log,
	}

	// Close should not panic
	err := client.Close()
	if err != nil {
		t.Errorf("Close should not return error for nil connection: %v", err)
	}

	// Second close should also be safe
	err = client.Close()
	if err != nil {
		t.Errorf("Second Close should not return error: %v", err)
	}
}

// =============================================================================
// Request/Response Types Tests
// =============================================================================

func TestClientRequest_Fields(t *testing.T) {
	req := &Request{
		Opcode:  OpGet,
		Payload: []byte("test"),
	}

	if req.Opcode != OpGet {
		t.Errorf("expected opcode %d, got %d", OpGet, req.Opcode)
	}
	if string(req.Payload) != "test" {
		t.Errorf("expected payload 'test', got '%s'", string(req.Payload))
	}
}

func TestClientResponse_Fields(t *testing.T) {
	resp := &Response{
		Status:  StatusOK,
		Payload: []byte("result"),
	}

	if resp.Status != StatusOK {
		t.Errorf("expected status %d, got %d", StatusOK, resp.Status)
	}
	if string(resp.Payload) != "result" {
		t.Errorf("expected payload 'result', got '%s'", string(resp.Payload))
	}
}

// =============================================================================
// Roundtrip Encoding Tests
// =============================================================================

func TestClientRequestEncodeDecode_Roundtrip(t *testing.T) {
	original := &Request{
		Opcode:  OpPut,
		Payload: []byte(`{"key":"value","number":123}`),
	}

	// Write to stream
	stream := NewMockStream(nil)
	err := WriteRequest(stream, original)
	if err != nil {
		t.Fatalf("WriteRequest failed: %v", err)
	}

	// Create a reader from the written data
	readStream := NewMockStream(stream.WrittenData())

	// Read back
	decoded, err := ReadRequest(readStream)
	if err != nil {
		t.Fatalf("ReadRequest failed: %v", err)
	}

	if decoded.Opcode != original.Opcode {
		t.Errorf("opcode mismatch: expected %d, got %d", original.Opcode, decoded.Opcode)
	}
	if string(decoded.Payload) != string(original.Payload) {
		t.Errorf("payload mismatch: expected '%s', got '%s'", string(original.Payload), string(decoded.Payload))
	}
}

func TestClientResponseEncodeDecode_Roundtrip(t *testing.T) {
	original := &Response{
		Status:  StatusOK,
		Payload: []byte(`{"success":true,"data":"result"}`),
	}

	// Write to stream
	stream := NewMockStream(nil)
	err := WriteResponse(stream, original)
	if err != nil {
		t.Fatalf("WriteResponse failed: %v", err)
	}

	// Create a reader from the written data
	readStream := NewMockStream(stream.WrittenData())

	// Read back
	decoded, err := ReadResponse(readStream)
	if err != nil {
		t.Fatalf("ReadResponse failed: %v", err)
	}

	if decoded.Status != original.Status {
		t.Errorf("status mismatch: expected %d, got %d", original.Status, decoded.Status)
	}
	if string(decoded.Payload) != string(original.Payload) {
		t.Errorf("payload mismatch: expected '%s', got '%s'", string(original.Payload), string(decoded.Payload))
	}
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestClientWriteRequest_LargePayload(t *testing.T) {
	// Create a large payload (1MB)
	largePayload := make([]byte, 1024*1024)
	for i := range largePayload {
		largePayload[i] = byte(i % 256)
	}

	stream := NewMockStream(nil)
	req := &Request{
		Opcode:  OpPut,
		Payload: largePayload,
	}

	err := WriteRequest(stream, req)
	if err != nil {
		t.Fatalf("WriteRequest failed for large payload: %v", err)
	}

	// Verify we can read it back
	readStream := NewMockStream(stream.WrittenData())
	decoded, err := ReadRequest(readStream)
	if err != nil {
		t.Fatalf("ReadRequest failed for large payload: %v", err)
	}

	if len(decoded.Payload) != len(largePayload) {
		t.Errorf("payload length mismatch: expected %d, got %d", len(largePayload), len(decoded.Payload))
	}
}

func TestClientWriteResponse_LargePayload(t *testing.T) {
	// Create a large payload (1MB)
	largePayload := make([]byte, 1024*1024)
	for i := range largePayload {
		largePayload[i] = byte(i % 256)
	}

	stream := NewMockStream(nil)
	resp := &Response{
		Status:  StatusOK,
		Payload: largePayload,
	}

	err := WriteResponse(stream, resp)
	if err != nil {
		t.Fatalf("WriteResponse failed for large payload: %v", err)
	}

	// Verify we can read it back
	readStream := NewMockStream(stream.WrittenData())
	decoded, err := ReadResponse(readStream)
	if err != nil {
		t.Fatalf("ReadResponse failed for large payload: %v", err)
	}

	if len(decoded.Payload) != len(largePayload) {
		t.Errorf("payload length mismatch: expected %d, got %d", len(largePayload), len(decoded.Payload))
	}
}

func TestClientReadRequest_TruncatedData(t *testing.T) {
	// Just the opcode, no length or payload
	stream := NewMockStream([]byte{OpGet})

	_, err := ReadRequest(stream)
	if err == nil {
		t.Fatal("expected error for truncated data")
	}
}

func TestClientReadResponse_TruncatedData(t *testing.T) {
	// Just the status, no length or payload
	stream := NewMockStream([]byte{StatusOK})

	_, err := ReadResponse(stream)
	if err == nil {
		t.Fatal("expected error for truncated data")
	}
}

func TestClientReadResponse_TruncatedPayload(t *testing.T) {
	// Status + length indicating 100 bytes, but only provide 5
	var buf bytes.Buffer
	buf.WriteByte(StatusOK)
	buf.Write([]byte{0, 0, 0, 100}) // 100 bytes expected
	buf.Write([]byte("12345"))      // Only 5 provided

	stream := NewMockStream(buf.Bytes())

	_, err := ReadResponse(stream)
	if err == nil {
		t.Fatal("expected error for truncated payload")
	}
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

func TestMockStream_ConcurrentReadWrite(t *testing.T) {
	stream := NewMockStream([]byte("initial data"))

	done := make(chan bool)

	// Reader goroutine
	go func() {
		buf := make([]byte, 5)
		for i := 0; i < 100; i++ {
			stream.Read(buf)
		}
		done <- true
	}()

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			stream.Write([]byte("data"))
		}
		done <- true
	}()

	// Wait for both
	<-done
	<-done
}

// =============================================================================
// Secrets Opcodes Tests
// =============================================================================

func TestClientSecretsOpcodes(t *testing.T) {
	secretsOpcodes := map[byte]string{
		OpSecretsStore:        "OpSecretsStore",
		OpSecretsGet:          "OpSecretsGet",
		OpSecretsList:         "OpSecretsList",
		OpSecretsDelete:       "OpSecretsDelete",
		OpSecretsListVersions: "OpSecretsListVersions",
		OpSecretsRollback:     "OpSecretsRollback",
		OpSecretsRotate:       "OpSecretsRotate",
		OpSecretsUpdateStatus: "OpSecretsUpdateStatus",
		OpSecretsGetByPrefix:  "OpSecretsGetByPrefix",
	}

	// Verify all secrets opcodes are unique
	reverseMap := make(map[byte][]string)
	for opcode, name := range secretsOpcodes {
		reverseMap[opcode] = append(reverseMap[opcode], name)
	}

	for opcode, names := range reverseMap {
		if len(names) > 1 {
			t.Errorf("duplicate secrets opcode %d: %v", opcode, names)
		}
	}
}

// =============================================================================
// Payload Encoding Tests
// =============================================================================

func TestClientEncodeDecodePayload_Roundtrip(t *testing.T) {
	type testData struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	original := &testData{Name: "test", Value: 42}

	// Encode
	payload, err := EncodePayload(original)
	if err != nil {
		t.Fatalf("EncodePayload failed: %v", err)
	}

	// Decode
	var decoded testData
	err = DecodePayload(payload, &decoded)
	if err != nil {
		t.Fatalf("DecodePayload failed: %v", err)
	}

	if decoded.Name != original.Name {
		t.Errorf("name mismatch: expected '%s', got '%s'", original.Name, decoded.Name)
	}
	if decoded.Value != original.Value {
		t.Errorf("value mismatch: expected %d, got %d", original.Value, decoded.Value)
	}
}

func TestClientDecodePayload_EmptyPayload(t *testing.T) {
	type testData struct {
		Name string `json:"name"`
	}

	var result testData
	err := DecodePayload([]byte{}, &result)
	// Empty payload should succeed with zero values
	if err != nil {
		t.Fatalf("DecodePayload failed for empty payload: %v", err)
	}
}

func TestClientDecodePayload_InvalidJSON(t *testing.T) {
	type testData struct {
		Name string `json:"name"`
	}

	var result testData
	err := DecodePayload([]byte("not json"), &result)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// =============================================================================
// Protocol Write/Read Tests
// =============================================================================

func TestClientWriteRequest_Basic(t *testing.T) {
	stream := NewMockStream(nil)

	req := &Request{
		Opcode:  OpPut,
		Payload: []byte(`{"test":"data"}`),
	}

	err := WriteRequest(stream, req)
	if err != nil {
		t.Fatalf("WriteRequest failed: %v", err)
	}

	written := stream.WrittenData()
	if len(written) == 0 {
		t.Fatal("no data written to stream")
	}

	// First byte should be opcode
	if written[0] != OpPut {
		t.Errorf("expected opcode %d, got %d", OpPut, written[0])
	}
}

func TestClientReadResponse_Basic(t *testing.T) {
	// Build a response manually
	responsePayload := []byte(`{"success":true}`)
	var buf bytes.Buffer
	buf.WriteByte(StatusOK)
	// Write 4-byte length in big endian
	length := uint32(len(responsePayload))
	buf.WriteByte(byte(length >> 24))
	buf.WriteByte(byte(length >> 16))
	buf.WriteByte(byte(length >> 8))
	buf.WriteByte(byte(length))
	buf.Write(responsePayload)

	stream := NewMockStream(buf.Bytes())

	resp, err := ReadResponse(stream)
	if err != nil {
		t.Fatalf("ReadResponse failed: %v", err)
	}
	if resp.Status != StatusOK {
		t.Errorf("expected status %d, got %d", StatusOK, resp.Status)
	}
	if string(resp.Payload) != string(responsePayload) {
		t.Errorf("expected payload '%s', got '%s'", string(responsePayload), string(resp.Payload))
	}
}

func TestClientReadResponse_EmptyPayload(t *testing.T) {
	// Build a response with empty payload
	var buf bytes.Buffer
	buf.WriteByte(StatusOK)
	// Zero length
	buf.Write([]byte{0, 0, 0, 0})

	stream := NewMockStream(buf.Bytes())

	resp, err := ReadResponse(stream)
	if err != nil {
		t.Fatalf("ReadResponse failed: %v", err)
	}
	if resp.Status != StatusOK {
		t.Errorf("expected status %d, got %d", StatusOK, resp.Status)
	}
	if len(resp.Payload) != 0 {
		t.Errorf("expected empty payload, got %d bytes", len(resp.Payload))
	}
}

func TestClientReadResponse_ErrorStatus(t *testing.T) {
	errorMsg := "key not found"
	var buf bytes.Buffer
	buf.WriteByte(StatusNotFound)
	length := uint32(len(errorMsg))
	buf.WriteByte(byte(length >> 24))
	buf.WriteByte(byte(length >> 16))
	buf.WriteByte(byte(length >> 8))
	buf.WriteByte(byte(length))
	buf.WriteString(errorMsg)

	stream := NewMockStream(buf.Bytes())

	resp, err := ReadResponse(stream)
	if err != nil {
		t.Fatalf("ReadResponse failed: %v", err)
	}
	if resp.Status != StatusNotFound {
		t.Errorf("expected status %d, got %d", StatusNotFound, resp.Status)
	}
}

// --- Content moved from client_coverage_91_test.go ---

// =============================================================================
// E2E Mock Service - extends mockService with working sub-services
// =============================================================================

// e2eMockService embeds the existing mockService and overrides sub-service
// accessors to return working mock implementations. It also implements
// service.SealService directly so server type assertions succeed.
type e2eMockService struct {
	mockService
	kmsSvc     service.KMSService
	secretsSvc service.SecretsService
	schedSvc   service.SchedulerService
}

func (e *e2eMockService) KMS() service.KMSService {
	return e.kmsSvc
}

func (e *e2eMockService) Secrets() service.SecretsService {
	return e.secretsSvc
}

func (e *e2eMockService) Scheduler() service.SchedulerService {
	return e.schedSvc
}

func (e *e2eMockService) SetQRDBClient(provider service.QRDBClientProvider) {
}

func (e *e2eMockService) Database() service.DatabaseService {
	return nil
}

func (e *e2eMockService) SetDatabaseService(ds service.DatabaseService) {
}

func (e *e2eMockService) Signal() interface{} {
	return nil
}

func (e *e2eMockService) SetSignalService(svc interface{}) {
}

// e2eBarrierService satisfies service.BarrierServicer for the in-process
// end-to-end tests. All methods return zero-value successes; specific tests
// can swap this fake out for a richer mock when needed.
type e2eBarrierService struct{}

func (s *e2eBarrierService) BarrierStatus(ctx context.Context) (*service.BarrierStatusInfo, error) {
	return &service.BarrierStatusInfo{Sealed: false, Strategy: "test"}, nil
}
func (s *e2eBarrierService) BarrierInitialize(ctx context.Context, secret string) error { return nil }
func (s *e2eBarrierService) BarrierUnseal(ctx context.Context, secret string) error     { return nil }
func (s *e2eBarrierService) BarrierSeal(ctx context.Context) error                      { return nil }
func (s *e2eBarrierService) BarrierInitializeShamir(ctx context.Context, secret string, threshold, total int) (*service.ShamirInitInfo, error) {
	return &service.ShamirInitInfo{Threshold: threshold, TotalShares: total, ShareCount: total}, nil
}
func (s *e2eBarrierService) BarrierUnsealWithShare(ctx context.Context, share string) (*service.UnsealShareProgress, error) {
	return &service.UnsealShareProgress{Required: 3, Submitted: 1, Complete: false}, nil
}
func (s *e2eBarrierService) BarrierUnsealWithShares(ctx context.Context, shares []string) error {
	return nil
}
func (s *e2eBarrierService) BarrierRekey(ctx context.Context, threshold, total int) (*service.ShamirInitInfo, error) {
	return &service.ShamirInitInfo{Threshold: threshold, TotalShares: total, ShareCount: total}, nil
}
func (s *e2eBarrierService) BarrierGenerateRecoveryKeys(ctx context.Context, threshold, total int) (*service.RecoveryKeysInfo, error) {
	return &service.RecoveryKeysInfo{Threshold: threshold, Total: total, ShareCount: total}, nil
}
func (s *e2eBarrierService) BarrierRecoverWithKeys(ctx context.Context, keys []string) error {
	return nil
}
func (s *e2eBarrierService) BarrierDeleteRecoveryKeys(ctx context.Context) error { return nil }
func (s *e2eBarrierService) BarrierHasRecoveryKeys(ctx context.Context) (bool, error) {
	return false, nil
}
func (s *e2eBarrierService) BarrierGenerateRootToken(ctx context.Context, shares []string) (*service.RootTokenInfo, error) {
	return &service.RootTokenInfo{Token: "tok"}, nil
}

func (s *e2eBarrierService) EnableEncryption(_ context.Context, _ string, _ qrdbsdk.MigrationOptions) error {
	return nil
}

// e2eCeremonyService satisfies service.CeremonyServicer for the in-process
// end-to-end tests.
type e2eCeremonyService struct{}

func (s *e2eCeremonyService) CeremonyInitialize(ctx context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
	return &service.CeremonyInitInfo{State: "initialized", ShareCount: req.Threshold}, nil
}
func (s *e2eCeremonyService) CeremonyStatus(ctx context.Context) (*service.CeremonyStatusInfo, error) {
	return &service.CeremonyStatusInfo{State: "initialized"}, nil
}
func (s *e2eCeremonyService) CeremonyBeginClaimCert(ctx context.Context, username string) (*service.CeremonyChallenge, error) {
	return &service.CeremonyChallenge{NonceHex: "deadbeef"}, nil
}
func (s *e2eCeremonyService) CeremonyCompleteClaimCert(ctx context.Context, username, nonceHex, signatureBase64 string) (*service.CeremonyClaimCertResult, error) {
	return &service.CeremonyClaimCertResult{CertPEM: []byte("CERT"), CACertPEM: []byte("CA")}, nil
}
func (s *e2eCeremonyService) CeremonyBeginClaimShare(ctx context.Context, username string) (*service.CeremonyChallenge, error) {
	return &service.CeremonyChallenge{NonceHex: "deadbeef"}, nil
}
func (s *e2eCeremonyService) CeremonyCompleteClaimShare(ctx context.Context, username, nonceHex string, signature []byte) (*service.CeremonyClaimShareResult, error) {
	return &service.CeremonyClaimShareResult{ShareIndex: 1, ShareJSON: "share-" + username}, nil
}
func (s *e2eCeremonyService) CeremonySignCSR(ctx context.Context, username, soPin, csrPEM, role string) (*service.CeremonySignCSRResult, error) {
	return &service.CeremonySignCSRResult{CertPEM: []byte("SIGNED")}, nil
}
func (s *e2eCeremonyService) CeremonyEnrollExternalOfficer(ctx context.Context, req *service.CeremonyEnrollExternalOfficerRequest) (*service.CeremonyEnrollExternalOfficerResponse, error) {
	return &service.CeremonyEnrollExternalOfficerResponse{SealedShare: "sealed"}, nil
}

// e2eSchedulerService implements the SchedulerService interface.
type e2eSchedulerService struct{}

func (s *e2eSchedulerService) CreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	return &service.CreateSchedulerJobResponse{}, nil
}
func (s *e2eSchedulerService) GetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	return &service.GetSchedulerJobResponse{}, nil
}
func (s *e2eSchedulerService) UpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	return &service.UpdateSchedulerJobResponse{}, nil
}
func (s *e2eSchedulerService) DeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	return &service.DeleteSchedulerJobResponse{}, nil
}
func (s *e2eSchedulerService) ListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	return &service.ListSchedulerJobsResponse{}, nil
}
func (s *e2eSchedulerService) TriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	return &service.TriggerSchedulerJobResponse{}, nil
}
func (s *e2eSchedulerService) PauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	return &service.PauseSchedulerJobResponse{}, nil
}
func (s *e2eSchedulerService) ResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	return &service.ResumeSchedulerJobResponse{}, nil
}
func (s *e2eSchedulerService) CancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	return &service.CancelSchedulerJobResponse{}, nil
}
func (s *e2eSchedulerService) GetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	return &service.GetSchedulerJobHistoryResponse{}, nil
}
func (s *e2eSchedulerService) GetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	return &service.GetSchedulerStatusResponse{}, nil
}

// e2eSecretsService implements service.SecretsService returning success.
type e2eSecretsService struct{}

func (s *e2eSecretsService) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	return &service.StoreSecretResponse{}, nil
}
func (s *e2eSecretsService) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	return &service.GetSecretResponse{}, nil
}
func (s *e2eSecretsService) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	return &service.ListSecretsResponse{}, nil
}
func (s *e2eSecretsService) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	return &service.DeleteSecretResponse{}, nil
}
func (s *e2eSecretsService) GetSecretVersion(ctx context.Context, req *service.GetSecretVersionRequest) (*service.GetSecretVersionResponse, error) {
	return &service.GetSecretVersionResponse{}, nil
}
func (s *e2eSecretsService) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	return &service.ListSecretVersionsResponse{}, nil
}
func (s *e2eSecretsService) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	return &service.RollbackSecretResponse{}, nil
}
func (s *e2eSecretsService) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	return &service.RotateSecretResponse{}, nil
}
func (s *e2eSecretsService) SetRotationPolicy(ctx context.Context, req *service.SetRotationPolicyRequest) (*service.SetRotationPolicyResponse, error) {
	return &service.SetRotationPolicyResponse{}, nil
}
func (s *e2eSecretsService) GetRotationPolicy(ctx context.Context, req *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error) {
	return &service.GetRotationPolicyResponse{}, nil
}
func (s *e2eSecretsService) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	return &service.UpdateSecretStatusResponse{}, nil
}
func (s *e2eSecretsService) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	return &service.GetSecretsByPrefixResponse{}, nil
}
func (s *e2eSecretsService) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	ch := make(chan *service.SecretChange)
	close(ch)
	return ch, nil
}

// =============================================================================
// E2E Test Infrastructure
// =============================================================================

// e2eTestEnv holds the server and client for end-to-end tests.
type e2eTestEnv struct {
	server *Server
	client *Client
	addr   string
}

// setupE2ETestEnv starts a QUIC server on a random port and connects a client.
func setupE2ETestEnv(t *testing.T) *e2eTestEnv {
	t.Helper()

	// Generate self-signed TLS config and fix ALPN protocol to match client
	serverTLS, serverCAPool := generateTestTLSConfigWithPool(t)
	serverTLS.NextProtos = []string{"quicbaas-quic"}

	// Find a free UDP port
	udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to resolve UDP addr: %v", err)
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		t.Fatalf("failed to listen UDP: %v", err)
	}
	port := udpConn.LocalAddr().(*net.UDPAddr).Port
	// Close so the server can bind to it
	udpConn.Close()

	addr := fmt.Sprintf("127.0.0.1:%d", port)

	// Create mock service with working sub-services
	svc := &e2eMockService{
		mockService: mockService{
			putFn: func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
				return &service.PutResponse{ID: 1}, nil
			},
			getFn: func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
				return &service.GetResponse{Data: []byte("value")}, nil
			},
			deleteFn: func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
				return &service.DeleteResponse{Deleted: true}, nil
			},
			listFn: func(ctx context.Context, req *service.ListRequest, stream chan<- *service.ListResponse) error {
				stream <- &service.ListResponse{Entities: []*service.Entity{}}
				close(stream)
				return nil
			},
			clusterStatusFn: func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
				return &service.ClusterStatusResponse{}, nil
			},
			getLeaderFn: func(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error) {
				return &service.GetLeaderResponse{}, nil
			},
			addNodeFn: func(ctx context.Context, req *service.AddNodeRequest) (*service.AddNodeResponse, error) {
				return &service.AddNodeResponse{}, nil
			},
			removeNodeFn: func(ctx context.Context, req *service.RemoveNodeRequest) (*service.RemoveNodeResponse, error) {
				return &service.RemoveNodeResponse{}, nil
			},
			createShardFn: func(ctx context.Context, req *service.CreateShardRequest) (*service.CreateShardResponse, error) {
				return &service.CreateShardResponse{}, nil
			},
			rebalanceClusterFn: func(ctx context.Context, req *service.RebalanceClusterRequest) (*service.RebalanceClusterResponse, error) {
				return &service.RebalanceClusterResponse{}, nil
			},
			healthFn: func(ctx context.Context, req *service.HealthRequest) (*service.HealthResponse, error) {
				return &service.HealthResponse{Status: service.HealthStatusHealthy}, nil
			},
			livenessFn: func(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error) {
				return &service.LivenessResponse{Alive: true}, nil
			},
			readinessFn: func(ctx context.Context, req *service.ReadinessRequest) (*service.ReadinessResponse, error) {
				return &service.ReadinessResponse{Ready: true}, nil
			},
		},
		kmsSvc:     &securityTestKMSService{},
		secretsSvc: &e2eSecretsService{},
		schedSvc:   &e2eSchedulerService{},
	}

	// Create and start server
	cfg := DefaultConfig()
	cfg.ListenAddr = addr
	cfg.TLSConfig = serverTLS
	cfg.MaxIdleTimeout = 10 * time.Second

	log := createServerTestLogger()
	server, err := NewServer(svc, cfg, log,
		WithServerBarrierService(&e2eBarrierService{}),
		WithServerCeremonyService(&e2eCeremonyService{}),
	)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Connect client
	clientTLS := &tls.Config{
		RootCAs:    serverCAPool,
		NextProtos: []string{"quicbaas-quic"},
		MinVersion: tls.VersionTLS13,
	}

	client, err := NewClient(addr,
		WithTLSConfig(clientTLS),
		WithTimeout(5*time.Second),
		WithMaxIdleTimeout(10*time.Second),
	)
	if err != nil {
		server.Stop()
		t.Fatalf("failed to create client: %v", err)
	}

	return &e2eTestEnv{
		server: server,
		client: client,
		addr:   addr,
	}
}

// teardown stops the server and closes the client.
func (env *e2eTestEnv) teardown(t *testing.T) {
	t.Helper()
	if env.client != nil {
		env.client.Close()
	}
	if env.server != nil {
		env.server.Stop()
	}
}

// =============================================================================
// Client E2E Tests - Data Operations
// =============================================================================

func TestClient91_DataOperations_E2E(t *testing.T) {
	env := setupE2ETestEnv(t)
	defer env.teardown(t)

	ctx := context.Background()

	t.Run("Put", func(t *testing.T) {
		resp, err := env.client.Put(ctx, &service.PutRequest{
			EntityType: "test",
			ID:         1,
			Data:       []byte("hello"),
		})
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("Get", func(t *testing.T) {
		resp, err := env.client.Get(ctx, &service.GetRequest{
			EntityType: "test",
			ID:         1,
		})
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		resp, err := env.client.Delete(ctx, &service.DeleteRequest{
			EntityType: "test",
			ID:         1,
		})
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("List", func(t *testing.T) {
		resp, err := env.client.List(ctx, &service.ListRequest{
			EntityType: "test",
		})
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})
}

// =============================================================================
// Client E2E Tests - KMS Operations
// =============================================================================

func TestClient91_KMSOperations_E2E(t *testing.T) {
	env := setupE2ETestEnv(t)
	defer env.teardown(t)

	ctx := context.Background()

	t.Run("KMSGenerateKey", func(t *testing.T) {
		resp, err := env.client.KMSGenerateKey(ctx, &service.GenerateKeyRequest{Name: "test-key"})
		if err != nil {
			t.Fatalf("KMSGenerateKey failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSImportKey", func(t *testing.T) {
		resp, err := env.client.KMSImportKey(ctx, &service.ImportKeyRequest{Name: "imported-key"})
		if err != nil {
			t.Fatalf("KMSImportKey failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSGetKey", func(t *testing.T) {
		resp, err := env.client.KMSGetKey(ctx, &service.GetKeyRequest{KeyID: "key-1"})
		if err != nil {
			t.Fatalf("KMSGetKey failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSListKeys", func(t *testing.T) {
		resp, err := env.client.KMSListKeys(ctx, &service.ListKeysRequest{})
		if err != nil {
			t.Fatalf("KMSListKeys failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSRotateKey", func(t *testing.T) {
		resp, err := env.client.KMSRotateKey(ctx, &service.RotateKeyRequest{KeyID: "key-1"})
		if err != nil {
			t.Fatalf("KMSRotateKey failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSDeleteKey", func(t *testing.T) {
		resp, err := env.client.KMSDeleteKey(ctx, &service.DeleteKeyRequest{KeyID: "key-1"})
		if err != nil {
			t.Fatalf("KMSDeleteKey failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSEncrypt", func(t *testing.T) {
		// Encrypt uses CryptoService which is nil; exercises client encode/send path
		_, _ = env.client.KMSEncrypt(ctx, &service.EncryptRequest{KeyID: "key-1", Plaintext: []byte("data")})
	})

	t.Run("KMSDecrypt", func(t *testing.T) {
		// Decrypt uses CryptoService which is nil; exercises client encode/send path
		_, _ = env.client.KMSDecrypt(ctx, &service.DecryptRequest{KeyID: "key-1", Ciphertext: []byte("enc")})
	})

	t.Run("KMSListKeyVersions", func(t *testing.T) {
		resp, err := env.client.KMSListKeyVersions(ctx, &service.ListKeyVersionsRequest{KeyID: "key-1"})
		if err != nil {
			t.Fatalf("KMSListKeyVersions failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSUpdateKeyStatus", func(t *testing.T) {
		resp, err := env.client.KMSUpdateKeyStatus(ctx, &service.UpdateKeyStatusRequest{KeyID: "key-1"})
		if err != nil {
			t.Fatalf("KMSUpdateKeyStatus failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSGenerateDataKey", func(t *testing.T) {
		// CryptoDataKey opcode is handled by crypto service which may be nil
		// This tests the client encode/send path regardless of server outcome
		_, _ = env.client.KMSGenerateDataKey(ctx, &service.GenerateDataKeyRequest{KeyID: "key-1"})
	})

	t.Run("KMSExportKey", func(t *testing.T) {
		resp, err := env.client.KMSExportKey(ctx, &service.ExportKeyRequest{KeyID: "key-1"})
		if err != nil {
			t.Fatalf("KMSExportKey failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSUnwrapKey", func(t *testing.T) {
		resp, err := env.client.KMSUnwrapKey(ctx, &service.UnwrapKeyRequest{WrappingKeyID: "key-1"})
		if err != nil {
			t.Fatalf("KMSUnwrapKey failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSSetKeySafety", func(t *testing.T) {
		resp, err := env.client.KMSSetKeySafety(ctx, &service.SetKeySafetyRequest{KeyID: "key-1"})
		if err != nil {
			t.Fatalf("KMSSetKeySafety failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSClearKeySafety", func(t *testing.T) {
		resp, err := env.client.KMSClearKeySafety(ctx, &service.ClearKeySafetyRequest{KeyID: "key-1"})
		if err != nil {
			t.Fatalf("KMSClearKeySafety failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSSafetyCheck", func(t *testing.T) {
		resp, err := env.client.KMSSafetyCheck(ctx, &service.SafetyCheckRequest{})
		if err != nil {
			t.Fatalf("KMSSafetyCheck failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSSafetyReport", func(t *testing.T) {
		resp, err := env.client.KMSSafetyReport(ctx, &service.SafetyReportRequest{})
		if err != nil {
			t.Fatalf("KMSSafetyReport failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})
}

// =============================================================================
// Client E2E Tests - DEK Operations
// =============================================================================

func TestClient91_DEKOperations_E2E(t *testing.T) {
	env := setupE2ETestEnv(t)
	defer env.teardown(t)

	ctx := context.Background()

	t.Run("KMSCreateDEK", func(t *testing.T) {
		resp, err := env.client.KMSCreateDEK(ctx, &service.CreateDEKRequest{Path: "dek/test"})
		if err != nil {
			t.Fatalf("KMSCreateDEK failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSGetDEK", func(t *testing.T) {
		resp, err := env.client.KMSGetDEK(ctx, &service.GetDEKRequest{Path: "dek/test"})
		if err != nil {
			t.Fatalf("KMSGetDEK failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSListDEKs", func(t *testing.T) {
		resp, err := env.client.KMSListDEKs(ctx, &service.ListDEKsRequest{})
		if err != nil {
			t.Fatalf("KMSListDEKs failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSRotateDEK", func(t *testing.T) {
		resp, err := env.client.KMSRotateDEK(ctx, &service.RotateDEKRequest{Path: "dek/test"})
		if err != nil {
			t.Fatalf("KMSRotateDEK failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSDeleteDEK", func(t *testing.T) {
		resp, err := env.client.KMSDeleteDEK(ctx, &service.DeleteDEKRequest{Path: "dek/test"})
		if err != nil {
			t.Fatalf("KMSDeleteDEK failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSGetDEKStatus", func(t *testing.T) {
		resp, err := env.client.KMSGetDEKStatus(ctx, &service.GetDEKStatusRequest{Path: "dek/test"})
		if err != nil {
			t.Fatalf("KMSGetDEKStatus failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSSetDefaultDEK", func(t *testing.T) {
		resp, err := env.client.KMSSetDefaultDEK(ctx, &service.SetDefaultDEKRequest{Path: "dek/test"})
		if err != nil {
			t.Fatalf("KMSSetDefaultDEK failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSSetDEKSafety", func(t *testing.T) {
		resp, err := env.client.KMSSetDEKSafety(ctx, &service.SetDEKSafetyRequest{Path: "dek/test"})
		if err != nil {
			t.Fatalf("KMSSetDEKSafety failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSClearDEKSafety", func(t *testing.T) {
		resp, err := env.client.KMSClearDEKSafety(ctx, &service.ClearDEKSafetyRequest{Path: "dek/test"})
		if err != nil {
			t.Fatalf("KMSClearDEKSafety failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})
}

// =============================================================================
// Client E2E Tests - Backend Operations
// =============================================================================

func TestClient91_BackendOperations_E2E(t *testing.T) {
	env := setupE2ETestEnv(t)
	defer env.teardown(t)

	ctx := context.Background()

	t.Run("KMSBackendStatus", func(t *testing.T) {
		resp, err := env.client.KMSBackendStatus(ctx)
		if err != nil {
			t.Fatalf("KMSBackendStatus failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("KMSBackendTest", func(t *testing.T) {
		resp, err := env.client.KMSBackendTest(ctx)
		if err != nil {
			t.Fatalf("KMSBackendTest failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})
}

// =============================================================================
// Client E2E Tests - Barrier Operations
// =============================================================================

func TestClient91_BarrierOperations_E2E(t *testing.T) {
	env := setupE2ETestEnv(t)
	defer env.teardown(t)

	ctx := context.Background()

	t.Run("BarrierStatus", func(t *testing.T) {
		resp, err := env.client.BarrierStatus(ctx)
		if err != nil {
			t.Fatalf("BarrierStatus failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("BarrierInitialize", func(t *testing.T) {
		err := env.client.BarrierInitialize(ctx, &BarrierInitializeRequest{Secret: "passphrase"})
		if err != nil {
			t.Fatalf("BarrierInitialize failed: %v", err)
		}
	})

	t.Run("BarrierShamirInitialize", func(t *testing.T) {
		resp, err := env.client.BarrierShamirInitialize(ctx, &BarrierShamirInitializeRequest{
			Secret: "passphrase", Threshold: 3, TotalShares: 5,
		})
		if err != nil {
			t.Fatalf("BarrierShamirInitialize failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("BarrierUnseal", func(t *testing.T) {
		err := env.client.BarrierUnseal(ctx, &BarrierUnsealRequest{Secret: "passphrase"})
		if err != nil {
			t.Fatalf("BarrierUnseal failed: %v", err)
		}
	})

	t.Run("BarrierSeal", func(t *testing.T) {
		if err := env.client.BarrierSeal(ctx); err != nil {
			t.Fatalf("BarrierSeal failed: %v", err)
		}
	})

	t.Run("BarrierRekey", func(t *testing.T) {
		resp, err := env.client.BarrierRekey(ctx, &BarrierRekeyRequest{Threshold: 2, Total: 3})
		if err != nil {
			t.Fatalf("BarrierRekey failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("BarrierShamirUnsealShare", func(t *testing.T) {
		resp, err := env.client.BarrierShamirUnsealShare(ctx, &BarrierShamirUnsealShareRequest{Share: "share1"})
		if err != nil {
			t.Fatalf("BarrierShamirUnsealShare failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("BarrierShamirUnsealShares", func(t *testing.T) {
		err := env.client.BarrierShamirUnsealShares(ctx, &BarrierShamirUnsealSharesRequest{
			Shares: []string{"s1", "s2", "s3"},
		})
		if err != nil {
			t.Fatalf("BarrierShamirUnsealShares failed: %v", err)
		}
	})

	t.Run("BarrierGenerateRecoveryKeys", func(t *testing.T) {
		resp, err := env.client.BarrierGenerateRecoveryKeys(ctx, &BarrierRecoveryGenerateRequest{Threshold: 2, Total: 3})
		if err != nil {
			t.Fatalf("BarrierGenerateRecoveryKeys failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("BarrierRecoverWithKeys", func(t *testing.T) {
		err := env.client.BarrierRecoverWithKeys(ctx, &BarrierRecoveryRecoverRequest{Keys: []string{"r1"}})
		if err != nil {
			t.Fatalf("BarrierRecoverWithKeys failed: %v", err)
		}
	})

	t.Run("BarrierDeleteRecoveryKeys", func(t *testing.T) {
		if err := env.client.BarrierDeleteRecoveryKeys(ctx); err != nil {
			t.Fatalf("BarrierDeleteRecoveryKeys failed: %v", err)
		}
	})

	t.Run("BarrierHasRecoveryKeys", func(t *testing.T) {
		resp, err := env.client.BarrierHasRecoveryKeys(ctx)
		if err != nil {
			t.Fatalf("BarrierHasRecoveryKeys failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("BarrierGenerateRootToken", func(t *testing.T) {
		resp, err := env.client.BarrierGenerateRootToken(ctx, &BarrierRootTokenRequest{Shares: []string{"s1"}})
		if err != nil {
			t.Fatalf("BarrierGenerateRootToken failed: %v", err)
		}
		if resp == nil || resp.Token == "" {
			t.Fatal("expected non-empty token in response")
		}
	})
}

// =============================================================================
// Client E2E Tests - Ceremony Operations
// =============================================================================

func TestClient91_CeremonyOperations_E2E(t *testing.T) {
	env := setupE2ETestEnv(t)
	defer env.teardown(t)

	ctx := context.Background()

	t.Run("CeremonyInitialize", func(t *testing.T) {
		resp, err := env.client.CeremonyInitialize(ctx, &CeremonyInitializeRequest{
			SOPin: "1234", UserPin: "5678", Threshold: 2,
		})
		if err != nil {
			t.Fatalf("CeremonyInitialize failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("CeremonyStatus", func(t *testing.T) {
		resp, err := env.client.CeremonyStatus(ctx)
		if err != nil {
			t.Fatalf("CeremonyStatus failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("CeremonyClaimCertBegin", func(t *testing.T) {
		resp, err := env.client.CeremonyClaimCertBegin(ctx, &CeremonyClaimCertBeginRequest{Username: "alice"})
		if err != nil {
			t.Fatalf("CeremonyClaimCertBegin failed: %v", err)
		}
		if resp == nil || resp.NonceHex == "" {
			t.Fatal("expected non-empty nonce in challenge response")
		}
	})

	t.Run("CeremonyClaimCertComplete", func(t *testing.T) {
		resp, err := env.client.CeremonyClaimCertComplete(ctx, &CeremonyClaimCertCompleteRequest{
			Username: "alice", NonceHex: "deadbeef", Signature: "sig",
		})
		if err != nil {
			t.Fatalf("CeremonyClaimCertComplete failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("CeremonyBeginClaimShare", func(t *testing.T) {
		resp, err := env.client.CeremonyBeginClaimShare(ctx, &CeremonyBeginClaimShareRequest{Username: "alice"})
		if err != nil {
			t.Fatalf("CeremonyBeginClaimShare failed: %v", err)
		}
		if resp == nil || resp.NonceHex == "" {
			t.Fatal("expected non-empty nonce in challenge response")
		}
	})

	t.Run("CeremonyCompleteClaimShare", func(t *testing.T) {
		resp, err := env.client.CeremonyCompleteClaimShare(ctx, &CeremonyCompleteClaimShareRequest{
			Username: "alice", NonceHex: "deadbeef", Signature: "c2ln",
		})
		if err != nil {
			t.Fatalf("CeremonyCompleteClaimShare failed: %v", err)
		}
		if resp == nil || resp.ShareJSON == "" {
			t.Fatal("expected non-empty share")
		}
	})

	t.Run("CeremonySignCSR", func(t *testing.T) {
		resp, err := env.client.CeremonySignCSR(ctx, &CeremonySignCSRRequest{
			Username: "alice", SOPin: "1234", CSRPEM: "csr", Role: "officer",
		})
		if err != nil {
			t.Fatalf("CeremonySignCSR failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("CeremonyListOfficers", func(t *testing.T) {
		resp, err := env.client.CeremonyListOfficers(ctx)
		if err != nil {
			t.Fatalf("CeremonyListOfficers failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("CeremonyEnrollExternalOfficer", func(t *testing.T) {
		resp, err := env.client.CeremonyEnrollExternalOfficer(ctx, &CeremonyEnrollExternalOfficerRequest{
			Username: "bob", PubKeyPEM: "PUBKEY", AuthFactor: "piv", ShareIndex: 2,
		})
		if err != nil {
			t.Fatalf("CeremonyEnrollExternalOfficer failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})
}

// =============================================================================
// Client E2E Tests - Secrets Operations
// =============================================================================

func TestClient91_SecretsOperations_E2E(t *testing.T) {
	env := setupE2ETestEnv(t)
	defer env.teardown(t)

	ctx := context.Background()

	t.Run("StoreSecret", func(t *testing.T) {
		resp, err := env.client.StoreSecret(ctx, &service.StoreSecretRequest{Name: "secret-1", Data: []byte("val")})
		if err != nil {
			t.Fatalf("StoreSecret failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("GetSecret", func(t *testing.T) {
		resp, err := env.client.GetSecret(ctx, &service.GetSecretRequest{SecretID: "secret-1"})
		if err != nil {
			t.Fatalf("GetSecret failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("ListSecrets", func(t *testing.T) {
		resp, err := env.client.ListSecrets(ctx, &service.ListSecretsRequest{Prefix: "secret/"})
		if err != nil {
			t.Fatalf("ListSecrets failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("DeleteSecret", func(t *testing.T) {
		resp, err := env.client.DeleteSecret(ctx, &service.DeleteSecretRequest{SecretID: "secret-1"})
		if err != nil {
			t.Fatalf("DeleteSecret failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("ListSecretVersions", func(t *testing.T) {
		resp, err := env.client.ListSecretVersions(ctx, &service.ListSecretVersionsRequest{SecretID: "secret-1"})
		if err != nil {
			t.Fatalf("ListSecretVersions failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("RollbackSecret", func(t *testing.T) {
		resp, err := env.client.RollbackSecret(ctx, &service.RollbackSecretRequest{SecretID: "secret-1", TargetVersion: 1})
		if err != nil {
			t.Fatalf("RollbackSecret failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("RotateSecret", func(t *testing.T) {
		resp, err := env.client.RotateSecret(ctx, &service.RotateSecretRequest{SecretID: "secret-1", NewData: []byte("new")})
		if err != nil {
			t.Fatalf("RotateSecret failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("UpdateSecretStatus", func(t *testing.T) {
		resp, err := env.client.UpdateSecretStatus(ctx, &service.UpdateSecretStatusRequest{SecretID: "secret-1", Status: "active"})
		if err != nil {
			t.Fatalf("UpdateSecretStatus failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("GetSecretsByPrefix", func(t *testing.T) {
		resp, err := env.client.GetSecretsByPrefix(ctx, &service.GetSecretsByPrefixRequest{Prefix: "secret/"})
		if err != nil {
			t.Fatalf("GetSecretsByPrefix failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})
}

// =============================================================================
// Client E2E Tests - Cluster Operations
// =============================================================================

func TestClient91_ClusterOperations_E2E(t *testing.T) {
	env := setupE2ETestEnv(t)
	defer env.teardown(t)

	ctx := context.Background()

	t.Run("ClusterStatus", func(t *testing.T) {
		resp, err := env.client.ClusterStatus(ctx, &service.ClusterStatusRequest{})
		if err != nil {
			t.Fatalf("ClusterStatus failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("GetLeader", func(t *testing.T) {
		resp, err := env.client.GetLeader(ctx, &service.GetLeaderRequest{ShardID: 1})
		if err != nil {
			t.Fatalf("GetLeader failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("AddNode", func(t *testing.T) {
		resp, err := env.client.AddNode(ctx, &service.AddNodeRequest{NodeID: 2, ShardID: 1})
		if err != nil {
			t.Fatalf("AddNode failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("RemoveNode", func(t *testing.T) {
		resp, err := env.client.RemoveNode(ctx, &service.RemoveNodeRequest{NodeID: 2, ShardID: 1})
		if err != nil {
			t.Fatalf("RemoveNode failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("CreateShard", func(t *testing.T) {
		resp, err := env.client.CreateShard(ctx, &service.CreateShardRequest{ShardID: 2})
		if err != nil {
			t.Fatalf("CreateShard failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("RebalanceCluster", func(t *testing.T) {
		resp, err := env.client.RebalanceCluster(ctx, &service.RebalanceClusterRequest{})
		if err != nil {
			t.Fatalf("RebalanceCluster failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})
}

// =============================================================================
// Client E2E Tests - Scheduler Operations
// =============================================================================

func TestClient91_SchedulerOperations_E2E(t *testing.T) {
	env := setupE2ETestEnv(t)
	defer env.teardown(t)

	ctx := context.Background()

	t.Run("SchedulerCreateJob", func(t *testing.T) {
		resp, err := env.client.SchedulerCreateJob(ctx, &service.CreateSchedulerJobRequest{Name: "test-job"})
		if err != nil {
			t.Fatalf("SchedulerCreateJob failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("SchedulerGetJob", func(t *testing.T) {
		resp, err := env.client.SchedulerGetJob(ctx, &service.GetSchedulerJobRequest{JobID: 1})
		if err != nil {
			t.Fatalf("SchedulerGetJob failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("SchedulerUpdateJob", func(t *testing.T) {
		resp, err := env.client.SchedulerUpdateJob(ctx, &service.UpdateSchedulerJobRequest{JobID: 1})
		if err != nil {
			t.Fatalf("SchedulerUpdateJob failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("SchedulerDeleteJob", func(t *testing.T) {
		resp, err := env.client.SchedulerDeleteJob(ctx, &service.DeleteSchedulerJobRequest{JobID: 1})
		if err != nil {
			t.Fatalf("SchedulerDeleteJob failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("SchedulerListJobs", func(t *testing.T) {
		resp, err := env.client.SchedulerListJobs(ctx, &service.ListSchedulerJobsRequest{})
		if err != nil {
			t.Fatalf("SchedulerListJobs failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("SchedulerTriggerJob", func(t *testing.T) {
		resp, err := env.client.SchedulerTriggerJob(ctx, &service.TriggerSchedulerJobRequest{JobID: 1})
		if err != nil {
			t.Fatalf("SchedulerTriggerJob failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("SchedulerPauseJob", func(t *testing.T) {
		resp, err := env.client.SchedulerPauseJob(ctx, &service.PauseSchedulerJobRequest{JobID: 1})
		if err != nil {
			t.Fatalf("SchedulerPauseJob failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("SchedulerResumeJob", func(t *testing.T) {
		resp, err := env.client.SchedulerResumeJob(ctx, &service.ResumeSchedulerJobRequest{JobID: 1})
		if err != nil {
			t.Fatalf("SchedulerResumeJob failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("SchedulerCancelJob", func(t *testing.T) {
		resp, err := env.client.SchedulerCancelJob(ctx, &service.CancelSchedulerJobRequest{JobID: 1})
		if err != nil {
			t.Fatalf("SchedulerCancelJob failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("SchedulerGetJobHistory", func(t *testing.T) {
		resp, err := env.client.SchedulerGetJobHistory(ctx, &service.GetSchedulerJobHistoryRequest{JobID: 1})
		if err != nil {
			t.Fatalf("SchedulerGetJobHistory failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})

	t.Run("SchedulerGetStatus", func(t *testing.T) {
		resp, err := env.client.SchedulerGetStatus(ctx)
		if err != nil {
			t.Fatalf("SchedulerGetStatus failed: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})
}

// =============================================================================
// Client Close and Connection Lifecycle Tests
// =============================================================================

func TestClient91_Close_IdempotentOnAlreadyClosed(t *testing.T) {
	env := setupE2ETestEnv(t)
	defer env.server.Stop()

	// First close should succeed
	err := env.client.Close()
	if err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	// Second close should be a no-op and not error
	err = env.client.Close()
	if err != nil {
		t.Fatalf("second Close should not error: %v", err)
	}
}

func TestClient91_Close_ThenSendRequest(t *testing.T) {
	env := setupE2ETestEnv(t)
	defer env.server.Stop()

	// Close the client first
	env.client.Close()

	// Attempting operations after close should return ClientClosedError
	_, err := env.client.Put(context.Background(), &service.PutRequest{EntityType: "test", ID: 1, Data: []byte("data")})
	if err == nil {
		t.Fatal("expected error after client close")
	}
}

// =============================================================================
// Client Options Tests
// =============================================================================

func TestClient91_WithMaxIdleTimeout(t *testing.T) {
	cfg := &clientConfig{}
	opt := WithMaxIdleTimeout(45 * time.Second)
	opt(cfg)
	if cfg.maxIdleTimeout != 45*time.Second {
		t.Errorf("expected 45s, got %v", cfg.maxIdleTimeout)
	}
}

func TestClient91_WithTimeout(t *testing.T) {
	cfg := &clientConfig{}
	opt := WithTimeout(30 * time.Second)
	opt(cfg)
	if cfg.timeout != 30*time.Second {
		t.Errorf("expected 30s, got %v", cfg.timeout)
	}
}
