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

package config

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultOllamaConfig(t *testing.T) {
	cfg := DefaultOllamaConfig()

	if cfg.Enabled {
		t.Error("expected default Enabled to be false")
	}

	if cfg.BaseURL != DefaultOllamaBaseURL {
		t.Errorf("expected BaseURL %q, got %s", DefaultOllamaBaseURL, cfg.BaseURL)
	}

	if cfg.DefaultEmbeddingModel != "nomic-embed-text" {
		t.Errorf("expected DefaultEmbeddingModel 'nomic-embed-text', got %s", cfg.DefaultEmbeddingModel)
	}

	if cfg.Timeout != 120*time.Second {
		t.Errorf("expected Timeout 120s, got %v", cfg.Timeout)
	}

	if cfg.MaxRetries != 3 {
		t.Errorf("expected MaxRetries 3, got %d", cfg.MaxRetries)
	}

	if cfg.RetryBackoff != time.Second {
		t.Errorf("expected RetryBackoff 1s, got %v", cfg.RetryBackoff)
	}

	if cfg.HealthCheckInterval != 30*time.Second {
		t.Errorf("expected HealthCheckInterval 30s, got %v", cfg.HealthCheckInterval)
	}
}

func TestOllamaConfig_Validate_Disabled(t *testing.T) {
	cfg := OllamaConfig{
		Enabled: false,
		// Other fields can be invalid when disabled
		BaseURL: "",
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error when disabled, got %v", err)
	}
}

func TestOllamaConfig_Validate_Valid(t *testing.T) {
	cfg := DefaultOllamaConfig()
	cfg.Enabled = true

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error for valid config, got %v", err)
	}
}

func TestOllamaConfig_Validate_EmptyBaseURL(t *testing.T) {
	cfg := OllamaConfig{
		Enabled:               true,
		BaseURL:               "",
		DefaultEmbeddingModel: "nomic-embed-text",
		Timeout:               time.Second,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty BaseURL")
	}

	if !strings.Contains(err.Error(), "ollama base_url cannot be empty") {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestOllamaConfig_Validate_InvalidURL(t *testing.T) {
	cfg := OllamaConfig{
		Enabled:               true,
		BaseURL:               "://invalid",
		DefaultEmbeddingModel: "nomic-embed-text",
		Timeout:               time.Second,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestOllamaConfig_Validate_InvalidScheme(t *testing.T) {
	cfg := OllamaConfig{
		Enabled:               true,
		BaseURL:               "ftp://localhost:11434",
		DefaultEmbeddingModel: "nomic-embed-text",
		Timeout:               time.Second,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid scheme")
	}

	if !strings.Contains(err.Error(), "ollama base_url must use http or https scheme") {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestOllamaConfig_Validate_NoHost(t *testing.T) {
	cfg := OllamaConfig{
		Enabled:               true,
		BaseURL:               "http:///path",
		DefaultEmbeddingModel: "nomic-embed-text",
		Timeout:               time.Second,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for missing host")
	}

	if !strings.Contains(err.Error(), "ollama base_url must include a host") {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestOllamaConfig_Validate_EmptyModel(t *testing.T) {
	cfg := OllamaConfig{
		Enabled:               true,
		BaseURL:               "http://localhost:11434",
		DefaultEmbeddingModel: "",
		Timeout:               time.Second,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for empty model")
	}

	if !strings.Contains(err.Error(), "ollama default_embedding_model cannot be empty") {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestOllamaConfig_Validate_InvalidTimeout(t *testing.T) {
	cfg := OllamaConfig{
		Enabled:               true,
		BaseURL:               "http://localhost:11434",
		DefaultEmbeddingModel: "nomic-embed-text",
		Timeout:               0,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for zero timeout")
	}

	if !strings.Contains(err.Error(), "ollama timeout must be positive") {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestOllamaConfig_Validate_NegativeTimeout(t *testing.T) {
	cfg := OllamaConfig{
		Enabled:               true,
		BaseURL:               "http://localhost:11434",
		DefaultEmbeddingModel: "nomic-embed-text",
		Timeout:               -time.Second,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative timeout")
	}
}

func TestOllamaConfig_Validate_NegativeMaxRetries(t *testing.T) {
	cfg := OllamaConfig{
		Enabled:               true,
		BaseURL:               "http://localhost:11434",
		DefaultEmbeddingModel: "nomic-embed-text",
		Timeout:               time.Second,
		MaxRetries:            -1,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative max retries")
	}

	if !strings.Contains(err.Error(), "ollama max_retries cannot be negative") {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestOllamaConfig_Validate_NegativeRetryBackoff(t *testing.T) {
	cfg := OllamaConfig{
		Enabled:               true,
		BaseURL:               "http://localhost:11434",
		DefaultEmbeddingModel: "nomic-embed-text",
		Timeout:               time.Second,
		MaxRetries:            0,
		RetryBackoff:          -time.Second,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative retry backoff")
	}

	if !strings.Contains(err.Error(), "ollama retry_backoff cannot be negative") {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestOllamaConfig_Validate_NegativeHealthCheckInterval(t *testing.T) {
	cfg := OllamaConfig{
		Enabled:               true,
		BaseURL:               "http://localhost:11434",
		DefaultEmbeddingModel: "nomic-embed-text",
		Timeout:               time.Second,
		MaxRetries:            0,
		RetryBackoff:          0,
		HealthCheckInterval:   -time.Second,
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative health check interval")
	}

	if !strings.Contains(err.Error(), "ollama health_check_interval cannot be negative") {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestOllamaConfig_Validate_HTTPSScheme(t *testing.T) {
	cfg := OllamaConfig{
		Enabled:               true,
		BaseURL:               "https://secure-ollama.example.com:11434",
		DefaultEmbeddingModel: "nomic-embed-text",
		Timeout:               time.Second,
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error for HTTPS URL, got %v", err)
	}
}

func TestOllamaConfig_MergeDefaults_Empty(t *testing.T) {
	cfg := OllamaConfig{}
	merged := cfg.MergeDefaults()

	defaults := DefaultOllamaConfig()

	if merged.BaseURL != defaults.BaseURL {
		t.Errorf("expected BaseURL %s, got %s", defaults.BaseURL, merged.BaseURL)
	}

	if merged.DefaultEmbeddingModel != defaults.DefaultEmbeddingModel {
		t.Errorf("expected DefaultEmbeddingModel %s, got %s", defaults.DefaultEmbeddingModel, merged.DefaultEmbeddingModel)
	}

	if merged.Timeout != defaults.Timeout {
		t.Errorf("expected Timeout %v, got %v", defaults.Timeout, merged.Timeout)
	}

	if merged.MaxRetries != defaults.MaxRetries {
		t.Errorf("expected MaxRetries %d, got %d", defaults.MaxRetries, merged.MaxRetries)
	}

	if merged.RetryBackoff != defaults.RetryBackoff {
		t.Errorf("expected RetryBackoff %v, got %v", defaults.RetryBackoff, merged.RetryBackoff)
	}

	if merged.HealthCheckInterval != defaults.HealthCheckInterval {
		t.Errorf("expected HealthCheckInterval %v, got %v", defaults.HealthCheckInterval, merged.HealthCheckInterval)
	}
}

func TestOllamaConfig_MergeDefaults_Partial(t *testing.T) {
	cfg := OllamaConfig{
		Enabled: true,
		BaseURL: "http://custom:11434",
		Timeout: 60 * time.Second,
	}
	merged := cfg.MergeDefaults()

	defaults := DefaultOllamaConfig()

	// Custom values preserved
	if merged.BaseURL != "http://custom:11434" {
		t.Errorf("expected custom BaseURL, got %s", merged.BaseURL)
	}

	if merged.Timeout != 60*time.Second {
		t.Errorf("expected custom Timeout, got %v", merged.Timeout)
	}

	// Defaults applied for missing values
	if merged.DefaultEmbeddingModel != defaults.DefaultEmbeddingModel {
		t.Errorf("expected default model, got %s", merged.DefaultEmbeddingModel)
	}

	if merged.MaxRetries != defaults.MaxRetries {
		t.Errorf("expected default MaxRetries, got %d", merged.MaxRetries)
	}

	if merged.RetryBackoff != defaults.RetryBackoff {
		t.Errorf("expected default RetryBackoff, got %v", merged.RetryBackoff)
	}

	if merged.HealthCheckInterval != defaults.HealthCheckInterval {
		t.Errorf("expected default HealthCheckInterval, got %v", merged.HealthCheckInterval)
	}
}

func TestOllamaConfig_MergeDefaults_Complete(t *testing.T) {
	cfg := OllamaConfig{
		Enabled:               true,
		BaseURL:               "http://custom:11434",
		DefaultEmbeddingModel: "custom-model",
		Timeout:               60 * time.Second,
		MaxRetries:            5,
		RetryBackoff:          2 * time.Second,
		HealthCheckInterval:   10 * time.Second,
	}
	merged := cfg.MergeDefaults()

	// All custom values preserved
	if merged.BaseURL != "http://custom:11434" {
		t.Errorf("expected custom BaseURL, got %s", merged.BaseURL)
	}

	if merged.DefaultEmbeddingModel != "custom-model" {
		t.Errorf("expected custom model, got %s", merged.DefaultEmbeddingModel)
	}

	if merged.Timeout != 60*time.Second {
		t.Errorf("expected custom Timeout, got %v", merged.Timeout)
	}

	if merged.MaxRetries != 5 {
		t.Errorf("expected custom MaxRetries, got %d", merged.MaxRetries)
	}

	if merged.RetryBackoff != 2*time.Second {
		t.Errorf("expected custom RetryBackoff, got %v", merged.RetryBackoff)
	}

	if merged.HealthCheckInterval != 10*time.Second {
		t.Errorf("expected custom HealthCheckInterval, got %v", merged.HealthCheckInterval)
	}
}

func TestConfigHasOllamaField(t *testing.T) {
	cfg := &Config{}
	cfg.Ollama = DefaultOllamaConfig()
	cfg.Ollama.Enabled = true

	if !cfg.Ollama.Enabled {
		t.Error("expected Ollama.Enabled to be true")
	}

	if cfg.Ollama.BaseURL != DefaultOllamaBaseURL {
		t.Errorf("expected default BaseURL, got %s", cfg.Ollama.BaseURL)
	}
}
