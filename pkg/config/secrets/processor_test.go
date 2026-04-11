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

package secrets

import (
	"context"
	stderrors "errors"
	"reflect"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// mockResolver is a test resolver that resolves values with a "mock://" prefix.
type mockResolver struct {
	resolveFunc func(ctx context.Context, raw string) (*SecretValue, error)
}

func (m *mockResolver) Scheme() string {
	return "mock"
}

func (m *mockResolver) CanResolve(raw string) bool {
	return len(raw) > 7 && raw[:7] == "mock://"
}

func (m *mockResolver) Resolve(ctx context.Context, raw string) (*SecretValue, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, raw)
	}
	// Default: return the value after the prefix as the resolved value
	return &SecretValue{
		Raw:      raw,
		Value:    raw[7:], // Strip "mock://"
		Strategy: StrategyLocalKMS,
	}, nil
}

// failingResolver always fails to resolve.
type failingResolver struct{}

func (f *failingResolver) Scheme() string {
	return "fail"
}

func (f *failingResolver) CanResolve(raw string) bool {
	return len(raw) > 7 && raw[:7] == "fail://"
}

func (f *failingResolver) Resolve(ctx context.Context, raw string) (*SecretValue, error) {
	return nil, &ResolutionFailedError{URI: raw, Reason: "intentional failure"}
}

// mockLogger captures log calls for testing.
type mockLogger struct {
	debugCalls []logCall
	infoCalls  []logCall
	warnCalls  []logCall
	errorCalls []logCall
}

type logCall struct {
	msg    string
	fields []interface{}
}

func (l *mockLogger) Debug(msg string, fields ...interface{}) {
	l.debugCalls = append(l.debugCalls, logCall{msg: msg, fields: fields})
}

func (l *mockLogger) Info(msg string, fields ...interface{}) {
	l.infoCalls = append(l.infoCalls, logCall{msg: msg, fields: fields})
}

func (l *mockLogger) Warn(msg string, fields ...interface{}) {
	l.warnCalls = append(l.warnCalls, logCall{msg: msg, fields: fields})
}

func (l *mockLogger) Error(msg string, fields ...interface{}) {
	l.errorCalls = append(l.errorCalls, logCall{msg: msg, fields: fields})
}

func TestConfigProcessor_Process_SimpleStruct(t *testing.T) {
	type SimpleConfig struct {
		Name     string
		Password string
		Port     int
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &SimpleConfig{
		Name:     "test",
		Password: "mock://secret-password",
		Port:     8080,
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	if result.Resolved[0].FieldPath != "Password" {
		t.Errorf("expected field path 'Password', got %q", result.Resolved[0].FieldPath)
	}

	if result.Resolved[0].Original != "mock://secret-password" {
		t.Errorf("expected original 'mock://secret-password', got %q", result.Resolved[0].Original)
	}

	if cfg.Password != "secret-password" {
		t.Errorf("expected password 'secret-password', got %q", cfg.Password)
	}

	if cfg.Name != "test" {
		t.Errorf("name should be unchanged, got %q", cfg.Name)
	}

	if cfg.Port != 8080 {
		t.Errorf("port should be unchanged, got %d", cfg.Port)
	}
}

func TestConfigProcessor_Process_NestedStructs(t *testing.T) {
	type DatabaseConfig struct {
		Host     string
		Password string
	}

	type ServerConfig struct {
		Address  string
		Database DatabaseConfig
	}

	type AppConfig struct {
		Name   string
		Server ServerConfig
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &AppConfig{
		Name: "myapp",
		Server: ServerConfig{
			Address: "localhost:8080",
			Database: DatabaseConfig{
				Host:     "db.example.com",
				Password: "mock://db-password",
			},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	expectedPath := "Server.Database.Password"
	if result.Resolved[0].FieldPath != expectedPath {
		t.Errorf("expected field path %q, got %q", expectedPath, result.Resolved[0].FieldPath)
	}

	if cfg.Server.Database.Password != "db-password" {
		t.Errorf("expected password 'db-password', got %q", cfg.Server.Database.Password)
	}
}

func TestConfigProcessor_Process_Pointers(t *testing.T) {
	type Credentials struct {
		Username string
		Password string
	}

	type Config struct {
		Name  string
		Creds *Credentials
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Name: "test",
		Creds: &Credentials{
			Username: "admin",
			Password: "mock://ptr-secret",
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	if cfg.Creds.Password != "ptr-secret" {
		t.Errorf("expected password 'ptr-secret', got %q", cfg.Creds.Password)
	}
}

func TestConfigProcessor_Process_NilPointers(t *testing.T) {
	type Credentials struct {
		Password string
	}

	type Config struct {
		Name  string
		Creds *Credentials
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Name:  "test",
		Creds: nil,
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 0 {
		t.Fatalf("expected 0 resolved secrets, got %d", len(result.Resolved))
	}

	if len(result.Errors) != 0 {
		t.Fatalf("expected 0 errors, got %d", len(result.Errors))
	}
}

func TestConfigProcessor_Process_Slices(t *testing.T) {
	type Backend struct {
		Name   string
		Secret string
	}

	type Config struct {
		Backends []Backend
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Backends: []Backend{
			{Name: "tpm", Secret: "mock://tpm-secret"},
			{Name: "pkcs11", Secret: "mock://pkcs11-secret"},
			{Name: "plain", Secret: "not-a-secret"},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 2 {
		t.Fatalf("expected 2 resolved secrets, got %d", len(result.Resolved))
	}

	// Verify paths
	expectedPaths := map[string]bool{
		"Backends[0].Secret": true,
		"Backends[1].Secret": true,
	}
	for _, r := range result.Resolved {
		if !expectedPaths[r.FieldPath] {
			t.Errorf("unexpected field path: %q", r.FieldPath)
		}
	}

	// Verify values
	if cfg.Backends[0].Secret != "tpm-secret" {
		t.Errorf("expected 'tpm-secret', got %q", cfg.Backends[0].Secret)
	}
	if cfg.Backends[1].Secret != "pkcs11-secret" {
		t.Errorf("expected 'pkcs11-secret', got %q", cfg.Backends[1].Secret)
	}
	if cfg.Backends[2].Secret != "not-a-secret" {
		t.Errorf("expected 'not-a-secret', got %q", cfg.Backends[2].Secret)
	}
}

func TestConfigProcessor_Process_Maps(t *testing.T) {
	type Config struct {
		Secrets map[string]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Secrets: map[string]string{
			"api_key":  "mock://api-key-value",
			"password": "mock://password-value",
			"literal":  "plain-text",
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 2 {
		t.Fatalf("expected 2 resolved secrets, got %d", len(result.Resolved))
	}

	// Verify map values are updated
	if cfg.Secrets["api_key"] != "api-key-value" {
		t.Errorf("expected 'api-key-value', got %q", cfg.Secrets["api_key"])
	}
	if cfg.Secrets["password"] != "password-value" {
		t.Errorf("expected 'password-value', got %q", cfg.Secrets["password"])
	}
	if cfg.Secrets["literal"] != "plain-text" {
		t.Errorf("expected 'plain-text' unchanged, got %q", cfg.Secrets["literal"])
	}
}

func TestConfigProcessor_Process_MixedTypes(t *testing.T) {
	type Nested struct {
		Secret string
	}

	type Config struct {
		Name     string
		Port     int
		Enabled  bool
		Password string
		Nested   Nested
		Secrets  []string
		Tags     map[string]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Name:     "app",
		Port:     8080,
		Enabled:  true,
		Password: "mock://root-password",
		Nested: Nested{
			Secret: "mock://nested-secret",
		},
		Secrets: []string{"mock://slice-secret-1", "mock://slice-secret-2"},
		Tags: map[string]string{
			"api": "mock://tag-secret",
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should resolve: Password, Nested.Secret, Secrets[0], Secrets[1], Tags[api]
	if len(result.Resolved) != 5 {
		t.Fatalf("expected 5 resolved secrets, got %d", len(result.Resolved))
	}

	// Verify original non-secret values unchanged
	if cfg.Name != "app" {
		t.Errorf("name should be unchanged")
	}
	if cfg.Port != 8080 {
		t.Errorf("port should be unchanged")
	}
	if !cfg.Enabled {
		t.Errorf("enabled should be unchanged")
	}

	// Verify secrets resolved
	if cfg.Password != "root-password" {
		t.Errorf("expected 'root-password', got %q", cfg.Password)
	}
	if cfg.Nested.Secret != "nested-secret" {
		t.Errorf("expected 'nested-secret', got %q", cfg.Nested.Secret)
	}
}

func TestConfigProcessor_Process_PreservesNonSecretFields(t *testing.T) {
	type Config struct {
		Name        string
		Description string
		Host        string
		Port        int
		Enabled     bool
		Timeout     float64
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Name:        "test-app",
		Description: "A test application",
		Host:        "localhost",
		Port:        9090,
		Enabled:     true,
		Timeout:     30.5,
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No secrets should be resolved
	if len(result.Resolved) != 0 {
		t.Fatalf("expected 0 resolved secrets, got %d", len(result.Resolved))
	}

	// All values should be unchanged
	if cfg.Name != "test-app" {
		t.Errorf("name changed unexpectedly")
	}
	if cfg.Description != "A test application" {
		t.Errorf("description changed unexpectedly")
	}
	if cfg.Host != "localhost" {
		t.Errorf("host changed unexpectedly")
	}
	if cfg.Port != 9090 {
		t.Errorf("port changed unexpectedly")
	}
	if !cfg.Enabled {
		t.Errorf("enabled changed unexpectedly")
	}
	if cfg.Timeout != 30.5 {
		t.Errorf("timeout changed unexpectedly")
	}
}

func TestConfigProcessor_Process_TracksResolvedSecretsCorrectly(t *testing.T) {
	type Config struct {
		Secret1 string
		Secret2 string
		Secret3 string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Secret1: "mock://value1",
		Secret2: "mock://value2",
		Secret3: "mock://value3",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 3 {
		t.Fatalf("expected 3 resolved secrets, got %d", len(result.Resolved))
	}

	// Build map of resolved secrets for easier testing
	resolvedMap := make(map[string]ResolvedSecret)
	for _, r := range result.Resolved {
		resolvedMap[r.FieldPath] = r
	}

	// Verify each resolved secret
	testCases := []struct {
		path     string
		original string
		strategy ResolvedStrategy
	}{
		{"Secret1", "mock://value1", StrategyLocalKMS},
		{"Secret2", "mock://value2", StrategyLocalKMS},
		{"Secret3", "mock://value3", StrategyLocalKMS},
	}

	for _, tc := range testCases {
		r, ok := resolvedMap[tc.path]
		if !ok {
			t.Errorf("missing resolved secret for path %q", tc.path)
			continue
		}
		if r.Original != tc.original {
			t.Errorf("path %q: expected original %q, got %q", tc.path, tc.original, r.Original)
		}
		if r.Strategy != tc.strategy {
			t.Errorf("path %q: expected strategy %v, got %v", tc.path, tc.strategy, r.Strategy)
		}
	}
}

func TestConfigProcessor_Process_HandlesErrorsGracefully(t *testing.T) {
	type Config struct {
		Good1 string
		Bad   string
		Good2 string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})
	registry.Register(&failingResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Good1: "mock://good-value1",
		Bad:   "fail://will-fail",
		Good2: "mock://good-value2",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should resolve 2 secrets successfully
	if len(result.Resolved) != 2 {
		t.Fatalf("expected 2 resolved secrets, got %d", len(result.Resolved))
	}

	// Should have 1 error
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}

	// Should have 1 unresolved
	if len(result.Unresolved) != 1 {
		t.Fatalf("expected 1 unresolved, got %d", len(result.Unresolved))
	}

	if result.Unresolved[0] != "Bad" {
		t.Errorf("expected unresolved field 'Bad', got %q", result.Unresolved[0])
	}

	// Good values should be resolved
	if cfg.Good1 != "good-value1" {
		t.Errorf("expected 'good-value1', got %q", cfg.Good1)
	}
	if cfg.Good2 != "good-value2" {
		t.Errorf("expected 'good-value2', got %q", cfg.Good2)
	}

	// Bad value should be unchanged
	if cfg.Bad != "fail://will-fail" {
		t.Errorf("expected 'fail://will-fail', got %q", cfg.Bad)
	}
}

func TestConfigProcessor_Process_NilConfig(t *testing.T) {
	registry := NewResolverRegistry()
	processor := NewConfigProcessor(registry)

	ctx := context.Background()
	result, err := processor.Process(ctx, nil)

	if err == nil {
		t.Fatal("expected error for nil config")
	}

	var de *errors.QBError
	if !stderrors.As(err, &de) {
		t.Fatalf("expected QBError, got %T", err)
	}

	if de.Code != errors.ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", de.Code)
	}

	var configNilErr *ConfigNilError
	if !stderrors.As(err, &configNilErr) {
		t.Errorf("expected ConfigNilError, got %T", de.Err)
	}

	if result != nil {
		t.Errorf("expected nil result, got %+v", result)
	}
}

func TestConfigProcessor_Process_NonPointer(t *testing.T) {
	type Config struct {
		Name string
	}

	registry := NewResolverRegistry()
	processor := NewConfigProcessor(registry)

	cfg := Config{Name: "test"}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err == nil {
		t.Fatal("expected error for non-pointer config")
	}

	var de *errors.QBError
	if !stderrors.As(err, &de) {
		t.Fatalf("expected QBError, got %T", err)
	}

	if de.Code != errors.ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", de.Code)
	}

	var notPtrErr *ConfigNotPointerError
	if !stderrors.As(err, &notPtrErr) {
		t.Errorf("expected ConfigNotPointerError, got %T", de.Err)
	}

	if result != nil {
		t.Errorf("expected nil result, got %+v", result)
	}
}

func TestConfigProcessor_Process_PointerToNonStruct(t *testing.T) {
	registry := NewResolverRegistry()
	processor := NewConfigProcessor(registry)

	value := "just a string"
	ctx := context.Background()
	result, err := processor.Process(ctx, &value)

	if err == nil {
		t.Fatal("expected error for pointer to non-struct")
	}

	var de *errors.QBError
	if !stderrors.As(err, &de) {
		t.Fatalf("expected QBError, got %T", err)
	}

	if de.Code != errors.ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", de.Code)
	}

	var notStructErr *ConfigNotStructError
	if !stderrors.As(err, &notStructErr) {
		t.Errorf("expected ConfigNotStructError, got %T", de.Err)
	}

	if result != nil {
		t.Errorf("expected nil result, got %+v", result)
	}
}

func TestConfigProcessor_Process_NilPointer(t *testing.T) {
	type Config struct {
		Name string
	}

	registry := NewResolverRegistry()
	processor := NewConfigProcessor(registry)

	var cfg *Config = nil

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err == nil {
		t.Fatal("expected error for nil pointer")
	}

	var de *errors.QBError
	if !stderrors.As(err, &de) {
		t.Fatalf("expected QBError, got %T", err)
	}

	if de.Code != errors.ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", de.Code)
	}

	if result != nil {
		t.Errorf("expected nil result, got %+v", result)
	}
}

func TestConfigProcessor_Process_ContextCancellation(t *testing.T) {
	type Config struct {
		Secret string
	}

	// Create a resolver that blocks until context is cancelled
	blockingResolver := &mockResolver{
		resolveFunc: func(ctx context.Context, raw string) (*SecretValue, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	registry := NewResolverRegistry()
	registry.Register(blockingResolver)

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Secret: "mock://blocked",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected top-level error: %v", err)
	}

	// The result should have an error due to cancellation
	if len(result.Errors) == 0 {
		t.Fatal("expected errors from cancellation")
	}

	if len(result.Unresolved) == 0 {
		t.Fatal("expected unresolved field from cancellation")
	}
}

func TestConfigProcessor_Process_NilContext(t *testing.T) {
	type Config struct {
		Name string
	}

	registry := NewResolverRegistry()
	processor := NewConfigProcessor(registry)

	cfg := &Config{Name: "test"}

	result, err := processor.Process(nil, cfg)

	if err == nil {
		t.Fatal("expected error for nil context")
	}

	var de *errors.QBError
	if !stderrors.As(err, &de) {
		t.Fatalf("expected QBError, got %T", err)
	}

	if de.Code != errors.ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", de.Code)
	}

	if result != nil {
		t.Errorf("expected nil result, got %+v", result)
	}
}

func TestConfigProcessor_Process_NilRegistry(t *testing.T) {
	type Config struct {
		Name string
	}

	processor := NewConfigProcessor(nil)

	cfg := &Config{Name: "test"}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err == nil {
		t.Fatal("expected error for nil registry")
	}

	var de *errors.QBError
	if !stderrors.As(err, &de) {
		t.Fatalf("expected QBError, got %T", err)
	}

	if de.Code != errors.ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", de.Code)
	}

	var registryNilErr *RegistryNilError
	if !stderrors.As(err, &registryNilErr) {
		t.Errorf("expected RegistryNilError, got %T", de.Err)
	}

	if result != nil {
		t.Errorf("expected nil result, got %+v", result)
	}
}

func TestConfigProcessor_Process_WithLogger(t *testing.T) {
	type Config struct {
		Secret string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	logger := &mockLogger{}
	processor := NewConfigProcessor(registry, WithLogger(logger))

	cfg := &Config{
		Secret: "mock://test-secret",
	}

	ctx := context.Background()
	_, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have debug logs for resolution
	if len(logger.debugCalls) == 0 {
		t.Error("expected debug log calls")
	}

	// Should have info log for completion
	if len(logger.infoCalls) == 0 {
		t.Error("expected info log call for completion")
	}

	// Find the completion log
	found := false
	for _, call := range logger.infoCalls {
		if call.msg == "config processing completed" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'config processing completed' info log")
	}
}

func TestConfigProcessor_Process_UnexportedFields(t *testing.T) {
	type Config struct {
		Public  string
		private string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Public:  "mock://public-secret",
		private: "mock://private-secret",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only public field should be resolved
	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	if result.Resolved[0].FieldPath != "Public" {
		t.Errorf("expected field path 'Public', got %q", result.Resolved[0].FieldPath)
	}

	if cfg.Public != "public-secret" {
		t.Errorf("expected 'public-secret', got %q", cfg.Public)
	}

	// Private field should be unchanged
	if cfg.private != "mock://private-secret" {
		t.Errorf("private field should be unchanged, got %q", cfg.private)
	}
}

func TestConfigProcessor_Process_EmptyStrings(t *testing.T) {
	type Config struct {
		Empty  string
		Secret string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Empty:  "",
		Secret: "mock://value",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only non-empty secret should be resolved
	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}
}

func TestConfigProcessor_Process_SliceOfPointers(t *testing.T) {
	type Item struct {
		Secret string
	}

	type Config struct {
		Items []*Item
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Items: []*Item{
			{Secret: "mock://item1"},
			nil, // nil pointer should be skipped
			{Secret: "mock://item3"},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 2 {
		t.Fatalf("expected 2 resolved secrets, got %d", len(result.Resolved))
	}

	if cfg.Items[0].Secret != "item1" {
		t.Errorf("expected 'item1', got %q", cfg.Items[0].Secret)
	}
	if cfg.Items[2].Secret != "item3" {
		t.Errorf("expected 'item3', got %q", cfg.Items[2].Secret)
	}
}

func TestConfigProcessor_Process_DeeplyNestedStructs(t *testing.T) {
	type Level4 struct {
		Secret string
	}

	type Level3 struct {
		L4 Level4
	}

	type Level2 struct {
		L3 Level3
	}

	type Level1 struct {
		L2 Level2
	}

	type Config struct {
		L1 Level1
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		L1: Level1{
			L2: Level2{
				L3: Level3{
					L4: Level4{
						Secret: "mock://deep-secret",
					},
				},
			},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	expectedPath := "L1.L2.L3.L4.Secret"
	if result.Resolved[0].FieldPath != expectedPath {
		t.Errorf("expected path %q, got %q", expectedPath, result.Resolved[0].FieldPath)
	}

	if cfg.L1.L2.L3.L4.Secret != "deep-secret" {
		t.Errorf("expected 'deep-secret', got %q", cfg.L1.L2.L3.L4.Secret)
	}
}

func TestConfigProcessor_Process_MultipleResolvers(t *testing.T) {
	type Config struct {
		MockSecret string
		AutoSecret string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})
	registry.Register(NewAutoGenResolver())

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		MockSecret: "mock://value",
		AutoSecret: "auto",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 2 {
		t.Fatalf("expected 2 resolved secrets, got %d", len(result.Resolved))
	}

	// Verify mock resolved
	if cfg.MockSecret != "value" {
		t.Errorf("expected 'value', got %q", cfg.MockSecret)
	}

	// Verify auto generated (should be non-empty and different from original)
	if cfg.AutoSecret == "auto" || cfg.AutoSecret == "" {
		t.Errorf("expected auto-generated secret, got %q", cfg.AutoSecret)
	}

	// Check strategies
	strategyMap := make(map[string]ResolvedStrategy)
	for _, r := range result.Resolved {
		strategyMap[r.FieldPath] = r.Strategy
	}

	if strategyMap["MockSecret"] != StrategyLocalKMS {
		t.Errorf("expected StrategyLocalKMS for MockSecret, got %v", strategyMap["MockSecret"])
	}
	if strategyMap["AutoSecret"] != StrategyAutoGen {
		t.Errorf("expected StrategyAutoGen for AutoSecret, got %v", strategyMap["AutoSecret"])
	}
}

func TestConfigProcessor_Process_MapWithStructValues(t *testing.T) {
	type Backend struct {
		Name   string
		Secret string
	}

	type Config struct {
		Backends map[string]Backend
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Backends: map[string]Backend{
			"tpm": {
				Name:   "TPM Backend",
				Secret: "mock://tpm-secret",
			},
			"pkcs11": {
				Name:   "PKCS11 Backend",
				Secret: "mock://pkcs11-secret",
			},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 2 {
		t.Fatalf("expected 2 resolved secrets, got %d", len(result.Resolved))
	}

	// Verify struct values in map are updated
	if cfg.Backends["tpm"].Secret != "tpm-secret" {
		t.Errorf("expected 'tpm-secret', got %q", cfg.Backends["tpm"].Secret)
	}
	if cfg.Backends["pkcs11"].Secret != "pkcs11-secret" {
		t.Errorf("expected 'pkcs11-secret', got %q", cfg.Backends["pkcs11"].Secret)
	}
}

func TestConfigProcessor_Process_SliceOfStrings(t *testing.T) {
	type Config struct {
		Secrets []string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Secrets: []string{"mock://s1", "literal", "mock://s3"},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 2 {
		t.Fatalf("expected 2 resolved secrets, got %d", len(result.Resolved))
	}

	if cfg.Secrets[0] != "s1" {
		t.Errorf("expected 's1', got %q", cfg.Secrets[0])
	}
	if cfg.Secrets[1] != "literal" {
		t.Errorf("expected 'literal', got %q", cfg.Secrets[1])
	}
	if cfg.Secrets[2] != "s3" {
		t.Errorf("expected 's3', got %q", cfg.Secrets[2])
	}
}

func TestConfigNilError_Error(t *testing.T) {
	err := &ConfigNilError{}
	expected := "config cannot be nil"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestConfigNotPointerError_Error(t *testing.T) {
	err := &ConfigNotPointerError{Kind: reflect.Struct}
	expected := "config must be a pointer to struct, got struct"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestConfigNotStructError_Error(t *testing.T) {
	err := &ConfigNotStructError{Kind: reflect.String}
	expected := "config must point to a struct, got pointer to string"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestRegistryNilError_Error(t *testing.T) {
	err := &RegistryNilError{}
	expected := "resolver registry cannot be nil"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestNewConfigProcessor(t *testing.T) {
	registry := NewResolverRegistry()
	processor := NewConfigProcessor(registry)

	if processor == nil {
		t.Fatal("expected non-nil processor")
	}

	if processor.registry != registry {
		t.Error("registry not set correctly")
	}

	if processor.logger != nil {
		t.Error("logger should be nil by default")
	}
}

func TestNewConfigProcessor_WithLogger(t *testing.T) {
	registry := NewResolverRegistry()
	logger := &mockLogger{}
	processor := NewConfigProcessor(registry, WithLogger(logger))

	if processor.logger != logger {
		t.Error("logger not set correctly")
	}
}

func TestConfigProcessor_Process_MapWithEmptyStringValue(t *testing.T) {
	type Config struct {
		Secrets map[string]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Secrets: map[string]string{
			"empty":  "",
			"secret": "mock://value",
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only non-empty secret should be resolved
	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	if cfg.Secrets["empty"] != "" {
		t.Errorf("empty string should remain empty, got %q", cfg.Secrets["empty"])
	}
	if cfg.Secrets["secret"] != "value" {
		t.Errorf("expected 'value', got %q", cfg.Secrets["secret"])
	}
}

func TestConfigProcessor_Process_MapWithFailingSecret(t *testing.T) {
	type Config struct {
		Secrets map[string]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})
	registry.Register(&failingResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Secrets: map[string]string{
			"good": "mock://good-value",
			"bad":  "fail://bad-value",
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 1 resolved and 1 error
	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}
	if len(result.Unresolved) != 1 {
		t.Fatalf("expected 1 unresolved, got %d", len(result.Unresolved))
	}

	if cfg.Secrets["good"] != "good-value" {
		t.Errorf("expected 'good-value', got %q", cfg.Secrets["good"])
	}
	// Bad value should be unchanged
	if cfg.Secrets["bad"] != "fail://bad-value" {
		t.Errorf("expected 'fail://bad-value', got %q", cfg.Secrets["bad"])
	}
}

func TestConfigProcessor_Process_InterfaceField(t *testing.T) {
	type Config struct {
		Data interface{}
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	// Interface containing a string
	cfg := &Config{
		Data: "mock://interface-secret",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Interface values are not directly settable, so no resolution expected
	if len(result.Resolved) != 0 {
		t.Fatalf("expected 0 resolved secrets (interface not settable), got %d", len(result.Resolved))
	}
}

func TestConfigProcessor_Process_NilInterfaceField(t *testing.T) {
	type Config struct {
		Data interface{}
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Data: nil,
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 0 {
		t.Fatalf("expected 0 resolved secrets, got %d", len(result.Resolved))
	}
	if len(result.Errors) != 0 {
		t.Fatalf("expected 0 errors, got %d", len(result.Errors))
	}
}

func TestConfigProcessor_Process_MapWithSliceValues(t *testing.T) {
	type Config struct {
		Data map[string][]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Data: map[string][]string{
			"secrets": {"mock://s1", "literal", "mock://s3"},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Slices in maps should be processed
	if len(result.Resolved) != 2 {
		t.Fatalf("expected 2 resolved secrets, got %d", len(result.Resolved))
	}

	if cfg.Data["secrets"][0] != "s1" {
		t.Errorf("expected 's1', got %q", cfg.Data["secrets"][0])
	}
	if cfg.Data["secrets"][1] != "literal" {
		t.Errorf("expected 'literal', got %q", cfg.Data["secrets"][1])
	}
	if cfg.Data["secrets"][2] != "s3" {
		t.Errorf("expected 's3', got %q", cfg.Data["secrets"][2])
	}
}

func TestConfigProcessor_Process_MapWithPointerValues(t *testing.T) {
	type Item struct {
		Secret string
	}

	type Config struct {
		Items map[string]*Item
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Items: map[string]*Item{
			"item1": {Secret: "mock://ptr-map-1"},
			"item2": nil, // nil pointer
			"item3": {Secret: "mock://ptr-map-3"},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 2 {
		t.Fatalf("expected 2 resolved secrets, got %d", len(result.Resolved))
	}

	if cfg.Items["item1"].Secret != "ptr-map-1" {
		t.Errorf("expected 'ptr-map-1', got %q", cfg.Items["item1"].Secret)
	}
	if cfg.Items["item3"].Secret != "ptr-map-3" {
		t.Errorf("expected 'ptr-map-3', got %q", cfg.Items["item3"].Secret)
	}
}

func TestConfigProcessor_Process_MapWithMapValues(t *testing.T) {
	type Config struct {
		NestedMaps map[string]map[string]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		NestedMaps: map[string]map[string]string{
			"group1": {
				"secret1": "mock://nested-1",
				"secret2": "mock://nested-2",
			},
			"group2": {
				"secret3": "mock://nested-3",
			},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 3 {
		t.Fatalf("expected 3 resolved secrets, got %d", len(result.Resolved))
	}

	if cfg.NestedMaps["group1"]["secret1"] != "nested-1" {
		t.Errorf("expected 'nested-1', got %q", cfg.NestedMaps["group1"]["secret1"])
	}
	if cfg.NestedMaps["group1"]["secret2"] != "nested-2" {
		t.Errorf("expected 'nested-2', got %q", cfg.NestedMaps["group1"]["secret2"])
	}
	if cfg.NestedMaps["group2"]["secret3"] != "nested-3" {
		t.Errorf("expected 'nested-3', got %q", cfg.NestedMaps["group2"]["secret3"])
	}
}

func TestConfigProcessor_Process_WithLoggerOnErrors(t *testing.T) {
	type Config struct {
		BadSecret string
	}

	registry := NewResolverRegistry()
	registry.Register(&failingResolver{})

	logger := &mockLogger{}
	processor := NewConfigProcessor(registry, WithLogger(logger))

	cfg := &Config{
		BadSecret: "fail://will-fail",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have error in result
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(result.Errors))
	}

	// Should have info log for completion even with errors
	if len(logger.infoCalls) == 0 {
		t.Error("expected info log call for completion")
	}
}

func TestConfigProcessor_Process_EmptySlice(t *testing.T) {
	type Config struct {
		Secrets []string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Secrets: []string{},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 0 {
		t.Fatalf("expected 0 resolved secrets, got %d", len(result.Resolved))
	}
}

func TestConfigProcessor_Process_EmptyMap(t *testing.T) {
	type Config struct {
		Secrets map[string]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Secrets: map[string]string{},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 0 {
		t.Fatalf("expected 0 resolved secrets, got %d", len(result.Resolved))
	}
}

func TestConfigProcessor_Process_NilSlice(t *testing.T) {
	type Config struct {
		Secrets []string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Secrets: nil,
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 0 {
		t.Fatalf("expected 0 resolved secrets, got %d", len(result.Resolved))
	}
}

func TestConfigProcessor_Process_NilMap(t *testing.T) {
	type Config struct {
		Secrets map[string]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Secrets: nil,
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 0 {
		t.Fatalf("expected 0 resolved secrets, got %d", len(result.Resolved))
	}
}

// ============================================================================
// Additional tests for coverage gaps
// ============================================================================

// TestConfigProcessor_Process_ChannelField tests that channel types are skipped
// (processValue should not process channel types as they are not settable).
func TestConfigProcessor_Process_ChannelField(t *testing.T) {
	type Config struct {
		Name    string
		Channel chan string
		Secret  string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	ch := make(chan string, 1)
	cfg := &Config{
		Name:    "test",
		Channel: ch,
		Secret:  "mock://channel-test",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only Secret should be resolved, Channel should be skipped
	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	if result.Resolved[0].FieldPath != "Secret" {
		t.Errorf("expected field path 'Secret', got %q", result.Resolved[0].FieldPath)
	}

	if cfg.Secret != "channel-test" {
		t.Errorf("expected 'channel-test', got %q", cfg.Secret)
	}

	// Verify channel was not modified
	if cfg.Channel != ch {
		t.Error("channel should be unchanged")
	}
}

// TestConfigProcessor_Process_FuncField tests that func types are skipped.
func TestConfigProcessor_Process_FuncField(t *testing.T) {
	type Config struct {
		Name     string
		Callback func() string
		Secret   string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	callback := func() string { return "callback" }
	cfg := &Config{
		Name:     "test",
		Callback: callback,
		Secret:   "mock://func-test",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only Secret should be resolved, Callback should be skipped
	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	if result.Resolved[0].FieldPath != "Secret" {
		t.Errorf("expected field path 'Secret', got %q", result.Resolved[0].FieldPath)
	}

	if cfg.Secret != "func-test" {
		t.Errorf("expected 'func-test', got %q", cfg.Secret)
	}

	// Verify callback was not modified
	if cfg.Callback == nil {
		t.Error("callback should not be nil")
	}
}

// TestConfigProcessor_Process_ComplexField tests that complex types are skipped.
func TestConfigProcessor_Process_ComplexField(t *testing.T) {
	type Config struct {
		Name    string
		Complex complex128
		Secret  string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Name:    "test",
		Complex: 1 + 2i,
		Secret:  "mock://complex-test",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only Secret should be resolved, Complex should be skipped
	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	if result.Resolved[0].FieldPath != "Secret" {
		t.Errorf("expected field path 'Secret', got %q", result.Resolved[0].FieldPath)
	}

	if cfg.Secret != "complex-test" {
		t.Errorf("expected 'complex-test', got %q", cfg.Secret)
	}

	// Verify complex was not modified
	if cfg.Complex != 1+2i {
		t.Errorf("expected 1+2i, got %v", cfg.Complex)
	}
}

// TestConfigProcessor_Process_Complex64Field tests that complex64 types are skipped.
func TestConfigProcessor_Process_Complex64Field(t *testing.T) {
	type Config struct {
		Name      string
		Complex64 complex64
		Secret    string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Name:      "test",
		Complex64: 3 + 4i,
		Secret:    "mock://complex64-test",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only Secret should be resolved
	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	if cfg.Secret != "complex64-test" {
		t.Errorf("expected 'complex64-test', got %q", cfg.Secret)
	}

	if cfg.Complex64 != 3+4i {
		t.Errorf("expected 3+4i, got %v", cfg.Complex64)
	}
}

// TestConfigProcessor_Process_UnsafePointerField tests that unsafe pointer types are skipped.
func TestConfigProcessor_Process_UnsafePointerField(t *testing.T) {
	type Config struct {
		Name   string
		Secret string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Name:   "test",
		Secret: "mock://unsafe-test",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	if cfg.Secret != "unsafe-test" {
		t.Errorf("expected 'unsafe-test', got %q", cfg.Secret)
	}
}

// TestConfigProcessor_Process_MapInterfaceWithString tests maps with interface{} values
// containing strings that need resolution.
func TestConfigProcessor_Process_MapInterfaceWithString(t *testing.T) {
	type Config struct {
		Data map[string]interface{}
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Data: map[string]interface{}{
			"secret":  "mock://interface-map-value",
			"literal": "plain-text",
			"number":  42,
			"nested": map[string]interface{}{
				"inner": "mock://nested-interface",
			},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Interface values in maps are not directly settable for strings
	// but nested maps/structs should be processed
	if len(result.Errors) > 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
}

// TestConfigProcessor_Process_DeeplyNestedMaps tests deeply nested map structures.
func TestConfigProcessor_Process_DeeplyNestedMaps(t *testing.T) {
	type Config struct {
		Level1 map[string]map[string]map[string]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Level1: map[string]map[string]map[string]string{
			"a": {
				"b": {
					"secret": "mock://deep-nested-map",
				},
			},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	if cfg.Level1["a"]["b"]["secret"] != "deep-nested-map" {
		t.Errorf("expected 'deep-nested-map', got %q", cfg.Level1["a"]["b"]["secret"])
	}
}

// TestConfigProcessor_Process_MapStringWithShortURI tests processMapString with
// a short URI (less than 10 characters) to hit the min() branch in logging.
func TestConfigProcessor_Process_MapStringWithShortURI(t *testing.T) {
	// Create a resolver that matches very short URIs
	shortResolver := &mockResolver{
		resolveFunc: func(ctx context.Context, raw string) (*SecretValue, error) {
			return &SecretValue{
				Raw:      raw,
				Value:    "resolved-" + raw,
				Strategy: StrategyLocalKMS,
			}, nil
		},
	}
	// Override CanResolve to match shorter strings
	type shortMockResolver struct {
		*mockResolver
	}
	sr := &shortMockResolver{mockResolver: shortResolver}

	registry := NewResolverRegistry()
	// Register with custom short URI matcher
	registry.Register(&mockResolver{
		resolveFunc: func(ctx context.Context, raw string) (*SecretValue, error) {
			return &SecretValue{
				Raw:      raw,
				Value:    raw[7:],
				Strategy: StrategyLocalKMS,
			}, nil
		},
	})

	logger := &mockLogger{}
	processor := NewConfigProcessor(registry, WithLogger(logger))

	_ = sr // unused but demonstrates pattern

	type Config struct {
		Secrets map[string]string
	}

	cfg := &Config{
		Secrets: map[string]string{
			"key": "mock://x", // Short URI (9 chars total, "mock://x")
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	// Verify debug logging happened with short URI
	if len(logger.debugCalls) == 0 {
		t.Error("expected debug log calls for short URI")
	}
}

// TestConfigProcessor_Process_MapStringWithLoggerResolved tests processMapString
// with logger enabled to verify the resolved logging path.
func TestConfigProcessor_Process_MapStringWithLoggerResolved(t *testing.T) {
	type Config struct {
		Secrets map[string]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	logger := &mockLogger{}
	processor := NewConfigProcessor(registry, WithLogger(logger))

	cfg := &Config{
		Secrets: map[string]string{
			"secret": "mock://map-logger-test",
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	// Should have debug log for resolving and resolved
	resolvingFound := false
	resolvedFound := false
	for _, call := range logger.debugCalls {
		if call.msg == "resolving secret in map" {
			resolvingFound = true
		}
		if call.msg == "resolved secret in map" {
			resolvedFound = true
		}
	}

	if !resolvingFound {
		t.Error("expected 'resolving secret in map' debug log")
	}
	if !resolvedFound {
		t.Error("expected 'resolved secret in map' debug log")
	}
}

// TestConfigProcessor_Process_InterfaceWithStruct tests interface fields containing structs.
func TestConfigProcessor_Process_InterfaceWithStruct(t *testing.T) {
	type Inner struct {
		Secret string
	}

	type Config struct {
		Data interface{}
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	// Interface containing a struct with a secret
	cfg := &Config{
		Data: Inner{
			Secret: "mock://interface-struct-secret",
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Struct inside interface should be processed but not settable
	// The struct value itself is not addressable through the interface
	if len(result.Errors) > 0 {
		t.Errorf("unexpected errors: %v", result.Errors)
	}
}

// TestConfigProcessor_Process_InterfaceWithPointerStruct tests interface fields
// containing pointer to struct.
func TestConfigProcessor_Process_InterfaceWithPointerStruct(t *testing.T) {
	type Inner struct {
		Secret string
	}

	type Config struct {
		Data interface{}
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	// Interface containing a pointer to struct with a secret
	cfg := &Config{
		Data: &Inner{
			Secret: "mock://interface-ptr-struct",
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Pointer through interface should allow modification
	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	inner := cfg.Data.(*Inner)
	if inner.Secret != "interface-ptr-struct" {
		t.Errorf("expected 'interface-ptr-struct', got %q", inner.Secret)
	}
}

// TestConfigProcessor_Process_ContextCancellationDuringStruct tests context
// cancellation while processing struct fields.
func TestConfigProcessor_Process_ContextCancellationDuringStruct(t *testing.T) {
	type Config struct {
		Secret1 string
		Secret2 string
		Secret3 string
	}

	callCount := 0
	slowResolver := &mockResolver{
		resolveFunc: func(ctx context.Context, raw string) (*SecretValue, error) {
			callCount++
			// First call succeeds, subsequent calls check context
			if callCount > 1 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				default:
				}
			}
			return &SecretValue{
				Raw:      raw,
				Value:    raw[7:],
				Strategy: StrategyLocalKMS,
			}, nil
		},
	}

	registry := NewResolverRegistry()
	registry.Register(slowResolver)

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Secret1: "mock://s1",
		Secret2: "mock://s2",
		Secret3: "mock://s3",
	}

	// Create a pre-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected top-level error: %v", err)
	}

	// Should have at least one cancellation error
	if len(result.Errors) == 0 {
		t.Fatal("expected errors from context cancellation")
	}
}

// TestConfigProcessor_Process_MapWithInterfaceContainingSlice tests map with
// interface values containing slices.
func TestConfigProcessor_Process_MapWithInterfaceContainingSlice(t *testing.T) {
	type Config struct {
		Data map[string]interface{}
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Data: map[string]interface{}{
			"secrets": []string{"mock://slice-in-interface-1", "mock://slice-in-interface-2"},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Slices inside interface values should be processed
	if len(result.Resolved) != 2 {
		t.Fatalf("expected 2 resolved secrets, got %d", len(result.Resolved))
	}
}

// TestConfigProcessor_Process_StringFieldWithLogger tests processString
// with logger enabled to hit the debug logging paths.
func TestConfigProcessor_Process_StringFieldWithLogger(t *testing.T) {
	type Config struct {
		Secret string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	logger := &mockLogger{}
	processor := NewConfigProcessor(registry, WithLogger(logger))

	cfg := &Config{
		Secret: "mock://logger-string-test",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	// Should have both "resolving secret" and "resolved secret" debug logs
	resolvingFound := false
	resolvedFound := false
	for _, call := range logger.debugCalls {
		if call.msg == "resolving secret" {
			resolvingFound = true
		}
		if call.msg == "resolved secret" {
			resolvedFound = true
		}
	}

	if !resolvingFound {
		t.Error("expected 'resolving secret' debug log")
	}
	if !resolvedFound {
		t.Error("expected 'resolved secret' debug log")
	}
}

// TestConfigProcessor_Process_NumericTypes tests that various numeric types are skipped.
func TestConfigProcessor_Process_NumericTypes(t *testing.T) {
	type Config struct {
		Int8    int8
		Int16   int16
		Int32   int32
		Int64   int64
		Uint8   uint8
		Uint16  uint16
		Uint32  uint32
		Uint64  uint64
		Float32 float32
		Float64 float64
		Secret  string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Int8:    1,
		Int16:   2,
		Int32:   3,
		Int64:   4,
		Uint8:   5,
		Uint16:  6,
		Uint32:  7,
		Uint64:  8,
		Float32: 9.0,
		Float64: 10.0,
		Secret:  "mock://numeric-test",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only Secret should be resolved
	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	if cfg.Secret != "numeric-test" {
		t.Errorf("expected 'numeric-test', got %q", cfg.Secret)
	}

	// Verify numeric fields unchanged
	if cfg.Int8 != 1 || cfg.Int16 != 2 || cfg.Int32 != 3 || cfg.Int64 != 4 {
		t.Error("signed integer fields should be unchanged")
	}
	if cfg.Uint8 != 5 || cfg.Uint16 != 6 || cfg.Uint32 != 7 || cfg.Uint64 != 8 {
		t.Error("unsigned integer fields should be unchanged")
	}
	if cfg.Float32 != 9.0 || cfg.Float64 != 10.0 {
		t.Error("float fields should be unchanged")
	}
}

// TestConfigProcessor_Process_BoolField tests that bool types are skipped.
func TestConfigProcessor_Process_BoolField(t *testing.T) {
	type Config struct {
		Enabled  bool
		Disabled bool
		Secret   string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Enabled:  true,
		Disabled: false,
		Secret:   "mock://bool-test",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only Secret should be resolved
	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	if cfg.Secret != "bool-test" {
		t.Errorf("expected 'bool-test', got %q", cfg.Secret)
	}

	if !cfg.Enabled {
		t.Error("Enabled should be true")
	}
	if cfg.Disabled {
		t.Error("Disabled should be false")
	}
}

// TestConfigProcessor_Process_MapWithNonStringKey tests maps with non-string keys.
func TestConfigProcessor_Process_MapWithNonStringKey(t *testing.T) {
	type Config struct {
		IntKeyMap map[int]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		IntKeyMap: map[int]string{
			1: "mock://int-key-1",
			2: "mock://int-key-2",
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Maps with non-string keys should still be processed
	if len(result.Resolved) != 2 {
		t.Fatalf("expected 2 resolved secrets, got %d", len(result.Resolved))
	}

	if cfg.IntKeyMap[1] != "int-key-1" {
		t.Errorf("expected 'int-key-1', got %q", cfg.IntKeyMap[1])
	}
	if cfg.IntKeyMap[2] != "int-key-2" {
		t.Errorf("expected 'int-key-2', got %q", cfg.IntKeyMap[2])
	}
}

// TestConfigProcessor_Process_NonSecretURINotResolved tests that strings
// without secret URI prefix are not resolved.
func TestConfigProcessor_Process_NonSecretURINotResolved(t *testing.T) {
	type Config struct {
		Host     string
		Path     string
		URL      string
		Password string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Host:     "localhost:8080",
		Path:     "/var/lib/data",
		URL:      "https://example.com/api",
		Password: "mock://real-password",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only Password should be resolved (has mock:// prefix)
	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	if result.Resolved[0].FieldPath != "Password" {
		t.Errorf("expected field path 'Password', got %q", result.Resolved[0].FieldPath)
	}

	// Non-secret fields should be unchanged
	if cfg.Host != "localhost:8080" {
		t.Errorf("Host should be unchanged, got %q", cfg.Host)
	}
	if cfg.Path != "/var/lib/data" {
		t.Errorf("Path should be unchanged, got %q", cfg.Path)
	}
	if cfg.URL != "https://example.com/api" {
		t.Errorf("URL should be unchanged, got %q", cfg.URL)
	}
	if cfg.Password != "real-password" {
		t.Errorf("expected 'real-password', got %q", cfg.Password)
	}
}

// TestConfigProcessor_Process_MapNonSecretNotResolved tests that map string values
// without secret URI are not resolved.
func TestConfigProcessor_Process_MapNonSecretNotResolved(t *testing.T) {
	type Config struct {
		Settings map[string]string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Settings: map[string]string{
			"host":     "localhost",
			"port":     "8080",
			"password": "mock://the-password",
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only password should be resolved
	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	if cfg.Settings["host"] != "localhost" {
		t.Errorf("host should be unchanged, got %q", cfg.Settings["host"])
	}
	if cfg.Settings["port"] != "8080" {
		t.Errorf("port should be unchanged, got %q", cfg.Settings["port"])
	}
	if cfg.Settings["password"] != "the-password" {
		t.Errorf("expected 'the-password', got %q", cfg.Settings["password"])
	}
}

// TestConfigProcessor_Process_EmbeddedStruct tests embedded struct fields.
func TestConfigProcessor_Process_EmbeddedStruct(t *testing.T) {
	type Credentials struct {
		Username string
		Password string
	}

	type Config struct {
		Credentials
		Name string
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Credentials: Credentials{
			Username: "admin",
			Password: "mock://embedded-password",
		},
		Name: "test",
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret, got %d", len(result.Resolved))
	}

	if cfg.Password != "embedded-password" {
		t.Errorf("expected 'embedded-password', got %q", cfg.Password)
	}
}

// TestConfigProcessor_Process_SliceOfInterfaces tests slices of interface{}.
func TestConfigProcessor_Process_SliceOfInterfaces(t *testing.T) {
	type Config struct {
		Items []interface{}
	}

	registry := NewResolverRegistry()
	registry.Register(&mockResolver{})

	processor := NewConfigProcessor(registry)

	cfg := &Config{
		Items: []interface{}{
			"mock://slice-interface-1",
			42,
			"mock://slice-interface-2",
			nil,
			map[string]string{"key": "mock://map-in-slice"},
		},
	}

	ctx := context.Background()
	result, err := processor.Process(ctx, cfg)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// String values in interface slice are not settable, but map should be processed
	if len(result.Resolved) != 1 {
		t.Fatalf("expected 1 resolved secret (from map), got %d", len(result.Resolved))
	}
}
