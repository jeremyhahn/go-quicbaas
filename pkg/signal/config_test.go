//go:build quicbaas_signal

package signal

import (
	"errors"
	"testing"
)

func TestSignalTenantConfig_Validate_Valid(t *testing.T) {
	cfg := &SignalTenantConfig{
		Enabled:          true,
		MaxAccounts:      100,
		MaxDevices:       6,
		MessageRetention: "30d",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestSignalTenantConfig_Validate_ValidHours(t *testing.T) {
	cfg := &SignalTenantConfig{
		MaxAccounts:      10,
		MaxDevices:       3,
		MessageRetention: "720h",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config with hours, got error: %v", err)
	}
}

func TestSignalTenantConfig_Validate_ZeroValues(t *testing.T) {
	cfg := &SignalTenantConfig{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected zero-value config to be valid, got error: %v", err)
	}
}

func TestSignalTenantConfig_Validate_NegativeMaxAccounts(t *testing.T) {
	cfg := &SignalTenantConfig{MaxAccounts: -1}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative MaxAccounts")
	}
	var target *InvalidConfigError
	if !errors.As(err, &target) {
		t.Fatalf("expected InvalidConfigError, got %T", err)
	}
	if target.Field != "maxAccounts" {
		t.Fatalf("expected field maxAccounts, got %s", target.Field)
	}
}

func TestSignalTenantConfig_Validate_NegativeMaxDevices(t *testing.T) {
	cfg := &SignalTenantConfig{MaxDevices: -5}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative MaxDevices")
	}
	var target *InvalidConfigError
	if !errors.As(err, &target) {
		t.Fatalf("expected InvalidConfigError, got %T", err)
	}
	if target.Field != "maxDevices" {
		t.Fatalf("expected field maxDevices, got %s", target.Field)
	}
}

func TestSignalTenantConfig_Validate_RetentionTooShort(t *testing.T) {
	cfg := &SignalTenantConfig{MessageRetention: "d"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for single-char retention")
	}
	var target *InvalidConfigError
	if !errors.As(err, &target) {
		t.Fatalf("expected InvalidConfigError, got %T", err)
	}
	if target.Field != "messageRetention" {
		t.Fatalf("expected field messageRetention, got %s", target.Field)
	}
}

func TestSignalTenantConfig_Validate_RetentionNoSuffix(t *testing.T) {
	cfg := &SignalTenantConfig{MessageRetention: "30x"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid retention suffix")
	}
	var target *InvalidConfigError
	if !errors.As(err, &target) {
		t.Fatalf("expected InvalidConfigError, got %T", err)
	}
}

func TestSignalTenantConfig_Validate_RetentionNonDigit(t *testing.T) {
	cfg := &SignalTenantConfig{MessageRetention: "3ad"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for non-digit in retention")
	}
	var target *InvalidConfigError
	if !errors.As(err, &target) {
		t.Fatalf("expected InvalidConfigError, got %T", err)
	}
}

func TestSignalTenantConfig_Validate_FIDO2OnlyWithoutEnabled(t *testing.T) {
	cfg := &SignalTenantConfig{
		AllowFIDO2Only: true,
		FIDO2Enabled:   false,
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for FIDO2Only without FIDO2Enabled")
	}
	var target *InvalidConfigError
	if !errors.As(err, &target) {
		t.Fatalf("expected InvalidConfigError, got %T", err)
	}
	if target.Field != "allowFido2Only" {
		t.Fatalf("expected field allowFido2Only, got %s", target.Field)
	}
}

func TestSignalTenantConfig_Validate_FIDO2OnlyWithEnabled(t *testing.T) {
	cfg := &SignalTenantConfig{
		AllowFIDO2Only: true,
		FIDO2Enabled:   true,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config with FIDO2 both set, got error: %v", err)
	}
}

func TestSignalTenantConfig_ApplyDefaults_SetsValues(t *testing.T) {
	cfg := &SignalTenantConfig{}
	cfg.ApplyDefaults()

	if cfg.MaxAccounts != defaultMaxAccounts {
		t.Fatalf("expected MaxAccounts %d, got %d", defaultMaxAccounts, cfg.MaxAccounts)
	}
	if cfg.MaxDevices != defaultMaxDevices {
		t.Fatalf("expected MaxDevices %d, got %d", defaultMaxDevices, cfg.MaxDevices)
	}
	if cfg.MessageRetention != defaultMessageRetention {
		t.Fatalf("expected MessageRetention %s, got %s", defaultMessageRetention, cfg.MessageRetention)
	}
}

func TestSignalTenantConfig_ApplyDefaults_DoesNotOverride(t *testing.T) {
	cfg := &SignalTenantConfig{
		MaxAccounts:      500,
		MaxDevices:       10,
		MessageRetention: "90d",
	}
	cfg.ApplyDefaults()

	if cfg.MaxAccounts != 500 {
		t.Fatalf("expected MaxAccounts 500, got %d", cfg.MaxAccounts)
	}
	if cfg.MaxDevices != 10 {
		t.Fatalf("expected MaxDevices 10, got %d", cfg.MaxDevices)
	}
	if cfg.MessageRetention != "90d" {
		t.Fatalf("expected MessageRetention 90d, got %s", cfg.MessageRetention)
	}
}

func TestValidateRetention_ValidDays(t *testing.T) {
	if err := validateRetention("30d"); err != nil {
		t.Fatalf("expected valid, got error: %v", err)
	}
}

func TestValidateRetention_ValidHours(t *testing.T) {
	if err := validateRetention("720h"); err != nil {
		t.Fatalf("expected valid, got error: %v", err)
	}
}

func TestValidateRetention_EmptySuffix(t *testing.T) {
	err := validateRetention("3")
	if err == nil {
		t.Fatal("expected error for single character retention")
	}
}

func TestValidateRetention_InvalidSuffix(t *testing.T) {
	err := validateRetention("30m")
	if err == nil {
		t.Fatal("expected error for invalid suffix 'm'")
	}
}
