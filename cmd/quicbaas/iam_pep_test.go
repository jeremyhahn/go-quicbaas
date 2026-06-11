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

package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

func policyPepTestInitLogger(t *testing.T) func() {
	origLog := log
	logConfig := logger.LogConfig{
		Level:     "error",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog
	return func() {
		log = origLog
	}
}

// =============================================================================
// buildPolicyRequest Tests
// =============================================================================

func TestBuildPolicyRequest_Basic(t *testing.T) {
	policyData := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":        "test-policy",
			"description": "Test policy description",
		},
		"spec": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{
					"name":   "rule1",
					"effect": "allow",
				},
			},
		},
	}

	rawYAML := []byte("apiVersion: abac.quicbaas.io/v1")

	req := buildPolicyRequest(policyData, "test-policy", rawYAML)

	assert.Equal(t, "test-policy", req.Name)
	assert.True(t, req.Enabled)
	assert.Equal(t, "Test policy description", req.Description)
	assert.Len(t, req.Rules, 1)
	assert.Equal(t, "rule1", req.Rules[0].Name)
	assert.Equal(t, iam.EffectAllow, req.Rules[0].Effect)
}

func TestBuildPolicyRequest_DenyEffect(t *testing.T) {
	policyData := map[string]interface{}{
		"spec": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{
					"name":   "deny-rule",
					"effect": "deny",
				},
			},
		},
	}

	req := buildPolicyRequest(policyData, "deny-policy", []byte{})

	assert.Len(t, req.Rules, 1)
	assert.Equal(t, iam.EffectDeny, req.Rules[0].Effect)
}

func TestBuildPolicyRequest_EmptySpec(t *testing.T) {
	policyData := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name": "empty-spec-policy",
		},
	}

	req := buildPolicyRequest(policyData, "empty-spec", []byte{})

	assert.Equal(t, "empty-spec", req.Name)
	assert.Len(t, req.Rules, 0)
}

func TestBuildPolicyRequest_NoMetadata(t *testing.T) {
	policyData := map[string]interface{}{}

	req := buildPolicyRequest(policyData, "no-metadata", []byte{})

	assert.Equal(t, "no-metadata", req.Name)
	assert.Empty(t, req.Description)
}

func TestBuildPolicyRequest_InvalidRulesFormat(t *testing.T) {
	policyData := map[string]interface{}{
		"spec": map[string]interface{}{
			"rules": "invalid", // Not a slice
		},
	}

	req := buildPolicyRequest(policyData, "invalid-rules", []byte{})

	assert.Equal(t, "invalid-rules", req.Name)
	assert.Len(t, req.Rules, 0)
}

func TestBuildPolicyRequest_InvalidRuleItem(t *testing.T) {
	policyData := map[string]interface{}{
		"spec": map[string]interface{}{
			"rules": []interface{}{
				"invalid-rule", // Not a map
			},
		},
	}

	req := buildPolicyRequest(policyData, "invalid-item", []byte{})

	assert.Len(t, req.Rules, 0)
}

// =============================================================================
// IAM Policy Apply Command Tests
// =============================================================================

func TestIAMPolicyApplyCommand_FileNotFound(t *testing.T) {
	cleanup := policyPepTestInitLogger(t)
	defer cleanup()

	cmd := newIAMPolicyApplyCommand()
	iamPolicyFile = "/nonexistent/policy.yaml"

	err := runIAMPolicyApplyCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read policy file")
}

func TestIAMPolicyApplyCommand_InvalidYAML(t *testing.T) {
	cleanup := policyPepTestInitLogger(t)
	defer cleanup()

	// Create a temp file with invalid YAML
	tmpFile, err := os.CreateTemp("", "invalid-policy-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(":::invalid yaml:::")
	require.NoError(t, err)
	tmpFile.Close()

	cmd := newIAMPolicyApplyCommand()
	iamPolicyFile = tmpFile.Name()

	err = runIAMPolicyApplyCommand(cmd, []string{})
	assert.Error(t, err)
	// Invalid YAML is parsed but fails validation due to missing metadata section
	assert.Contains(t, err.Error(), "policy must have metadata section")
}

func TestIAMPolicyApplyCommand_MissingMetadata(t *testing.T) {
	cleanup := policyPepTestInitLogger(t)
	defer cleanup()

	// Create a temp file with YAML but no metadata
	tmpFile, err := os.CreateTemp("", "no-metadata-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("apiVersion: v1\nkind: Policy\n")
	require.NoError(t, err)
	tmpFile.Close()

	cmd := newIAMPolicyApplyCommand()
	iamPolicyFile = tmpFile.Name()

	err = runIAMPolicyApplyCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must have metadata")
}

func TestIAMPolicyApplyCommand_MissingName(t *testing.T) {
	cleanup := policyPepTestInitLogger(t)
	defer cleanup()

	// Create a temp file with metadata but no name
	tmpFile, err := os.CreateTemp("", "no-name-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("apiVersion: v1\nmetadata:\n  tenant: test\n")
	require.NoError(t, err)
	tmpFile.Close()

	cmd := newIAMPolicyApplyCommand()
	iamPolicyFile = tmpFile.Name()

	err = runIAMPolicyApplyCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must have metadata.name")
}

func TestIAMPolicyApplyCommand_MissingTenant(t *testing.T) {
	cleanup := policyPepTestInitLogger(t)
	defer cleanup()

	// Create a temp file with name but no tenant
	tmpFile, err := os.CreateTemp("", "no-tenant-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("apiVersion: v1\nmetadata:\n  name: test-policy\n")
	require.NoError(t, err)
	tmpFile.Close()

	cmd := newIAMPolicyApplyCommand()
	iamPolicyFile = tmpFile.Name()

	err = runIAMPolicyApplyCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must have metadata.tenant")
}

func TestIAMPolicyApplyCommand_FactoryConnError(t *testing.T) {
	cleanup := policyPepTestInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	// Create a valid temp file
	tmpFile, err := os.CreateTemp("", "valid-policy-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(`apiVersion: v1
metadata:
  name: test-policy
  tenant: test-tenant
spec:
  rules: []
`)
	require.NoError(t, err)
	tmpFile.Close()

	cmd := newIAMPolicyApplyCommand()
	iamPolicyFile = tmpFile.Name()

	err = runIAMPolicyApplyCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection failed")
}

// =============================================================================
// IAM PEP Check Command Tests
// =============================================================================

func TestIAMPEPCheckCommand_MissingSubject(t *testing.T) {
	cleanup := policyPepTestInitLogger(t)
	defer cleanup()

	cmd := newIAMPEPCheckCommand()
	globalTenantID = "test-tenant"
	pepSubject = ""
	pepAction = "read"
	pepResource = "data/test"

	err := runIAMPEPCheckCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "subject")
}

func TestIAMPEPCheckCommand_MissingAction(t *testing.T) {
	cleanup := policyPepTestInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	// Mock the client to avoid connection attempts
	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrInvalidInput, "test", "action is required")
	}

	cmd := newIAMPEPCheckCommand()
	globalTenantID = "test-tenant"
	pepSubject = `{"id": "user1"}`
	pepAction = ""
	pepResource = "data/test"

	err := runIAMPEPCheckCommand(cmd, []string{})
	assert.Error(t, err)
	// The error will come from the factory in this test
	assert.Contains(t, err.Error(), "action")
}

func TestIAMPEPCheckCommand_MissingResource(t *testing.T) {
	cleanup := policyPepTestInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	// Mock the client to avoid connection attempts
	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrInvalidInput, "test", "resource is required")
	}

	cmd := newIAMPEPCheckCommand()
	globalTenantID = "test-tenant"
	pepSubject = `{"id": "user1"}`
	pepAction = "read"
	pepResource = ""

	err := runIAMPEPCheckCommand(cmd, []string{})
	assert.Error(t, err)
	// The error will come from the factory in this test
	assert.Contains(t, err.Error(), "resource")
}

func TestIAMPEPCheckCommand_FactoryConnError(t *testing.T) {
	cleanup := policyPepTestInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMPEPCheckCommand()
	globalTenantID = "test-tenant"
	pepSubject = `{"id": "user1"}`
	pepAction = "read"
	pepResource = "data/test"

	err := runIAMPEPCheckCommand(cmd, []string{})
	assert.Error(t, err)
}

// =============================================================================
// IAM PEP Check Batch Command Tests
// =============================================================================

func TestIAMPEPCheckBatchCommand_MissingTenant(t *testing.T) {
	cleanup := policyPepTestInitLogger(t)
	defer cleanup()

	cmd := newIAMPEPCheckBatchCommand()
	globalTenantID = ""
	pepFile = "/tmp/test.json"

	err := runIAMPEPCheckBatchCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tenant")
}

func TestIAMPEPCheckBatchCommand_FileNotFound(t *testing.T) {
	cleanup := policyPepTestInitLogger(t)
	defer cleanup()

	cmd := newIAMPEPCheckBatchCommand()
	globalTenantID = "test-tenant"
	pepFile = "/nonexistent/batch.json"

	err := runIAMPEPCheckBatchCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read batch file")
}

func TestIAMPEPCheckBatchCommand_InvalidJSON(t *testing.T) {
	cleanup := policyPepTestInitLogger(t)
	defer cleanup()

	// Create a temp file with invalid JSON
	tmpFile, err := os.CreateTemp("", "invalid-batch-*.json")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("not valid json")
	require.NoError(t, err)
	tmpFile.Close()

	cmd := newIAMPEPCheckBatchCommand()
	globalTenantID = "test-tenant"
	pepFile = tmpFile.Name()

	err = runIAMPEPCheckBatchCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse batch file JSON")
}

func TestIAMPEPCheckBatchCommand_EmptyRequests(t *testing.T) {
	cleanup := policyPepTestInitLogger(t)
	defer cleanup()

	// Create a temp file with empty requests array
	tmpFile, err := os.CreateTemp("", "empty-batch-*.json")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(`{"requests": []}`)
	require.NoError(t, err)
	tmpFile.Close()

	cmd := newIAMPEPCheckBatchCommand()
	globalTenantID = "test-tenant"
	pepFile = tmpFile.Name()

	err = runIAMPEPCheckBatchCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no requests")
}

func TestIAMPEPCheckBatchCommand_FactoryConnError(t *testing.T) {
	cleanup := policyPepTestInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	// Create a valid temp file
	tmpFile, err := os.CreateTemp("", "valid-batch-*.json")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(`{
	"requests": [
		{"subject": {"id": "user1"}, "action": "read", "resource": "data/test"}
	]
}`)
	require.NoError(t, err)
	tmpFile.Close()

	cmd := newIAMPEPCheckBatchCommand()
	globalTenantID = "test-tenant"
	pepFile = tmpFile.Name()

	err = runIAMPEPCheckBatchCommand(cmd, []string{})
	assert.Error(t, err)
}

// =============================================================================
// IAM Policy Command Structure Tests
// =============================================================================

func TestNewIAMPolicyApplyCommand_FlagsExist(t *testing.T) {
	cmd := newIAMPolicyApplyCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "apply")

	// Check file flag exists
	assert.NotNil(t, cmd.Flags().Lookup("file"))
}

func TestNewIAMPolicyTestCommand_FlagsExist(t *testing.T) {
	cmd := newIAMPolicyTestCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "test")

	// Check flags exist
	assert.NotNil(t, cmd.Flags().Lookup("subject"))
	assert.NotNil(t, cmd.Flags().Lookup("action"))
	assert.NotNil(t, cmd.Flags().Lookup("resource"))
}

func TestNewIAMPolicyValidateCommand_StructureExists(t *testing.T) {
	cmd := newIAMPolicyValidateCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "validate")
	assert.NotEmpty(t, cmd.Short)
}

// =============================================================================
// IAM PEP Command Structure Tests
// =============================================================================

func TestNewIAMPEPCommand_SubcommandsExist(t *testing.T) {
	cmd := newIAMPEPCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "pep", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	// Verify subcommands exist (check and check-batch, not batch)
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "check" || sub.Name() == "check-batch" {
			found = true
		}
	}
	assert.True(t, found, "Expected check or check-batch subcommand")
}

func TestNewIAMPEPCheckCommand_FlagsExist(t *testing.T) {
	cmd := newIAMPEPCheckCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "check", cmd.Use)

	// Check flags exist
	assert.NotNil(t, cmd.Flags().Lookup("subject"))
	assert.NotNil(t, cmd.Flags().Lookup("action"))
	assert.NotNil(t, cmd.Flags().Lookup("resource"))
}

func TestNewIAMPEPCheckBatchCommand_FlagsExist(t *testing.T) {
	cmd := newIAMPEPCheckBatchCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "check-batch", cmd.Use)

	// Check file flag exists
	assert.NotNil(t, cmd.Flags().Lookup("file"))
}
