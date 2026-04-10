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

// Package main provides tests for role management CLI commands.
package main

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rbac"
)

func TestRoleCreateCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		mockFactory func() (rbac.Client, error)
		expectError bool
		errorMsg    string
	}{
		{
			name: "create custom role",
			args: []string{
				"backup-operator",
				"--description", "Backup operations",
				"--permissions", "backups:read,backups:write,backups:delete",
			},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					CreateRoleFunc: func(ctx context.Context, req *rbac.CreateRoleRequest) (*rbac.CreateRoleResponse, error) {
						return &rbac.CreateRoleResponse{
							RoleID:    "role-123",
							Timestamp: time.Now().UnixNano(),
						}, nil
					},
				}, nil
			},
			expectError: false,
		},
		{
			name: "create role without permissions",
			args: []string{
				"test-role",
				"--description", "Test role",
			},
			mockFactory: func() (rbac.Client, error) {
				t.Fatal("should not call factory")
				return nil, nil
			},
			expectError: true,
			errorMsg:    "required flag(s) \"permissions\" not set",
		},
		{
			name: "create role with reserved name",
			args: []string{
				"admin",
				"--permissions", "keys:read",
			},
			mockFactory: func() (rbac.Client, error) {
				t.Fatal("should not call factory")
				return nil, nil
			},
			expectError: true,
			errorMsg:    "cannot create role with reserved name",
		},
		{
			name: "create role with invalid permission format",
			args: []string{
				"test-role",
				"--permissions", "invalid-format",
			},
			mockFactory: func() (rbac.Client, error) {
				t.Fatal("should not call factory")
				return nil, nil
			},
			expectError: true,
			errorMsg:    "invalid permission format",
		},
		{
			name: "create role with wildcard permissions",
			args: []string{
				"super-admin",
				"--permissions", "*:*",
			},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					CreateRoleFunc: func(ctx context.Context, req *rbac.CreateRoleRequest) (*rbac.CreateRoleResponse, error) {
						return &rbac.CreateRoleResponse{
							RoleID:    "role-456",
							Timestamp: time.Now().UnixNano(),
						}, nil
					},
				}, nil
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore factory
			origFactory := rbacClientFactory
			defer func() { rbacClientFactory = origFactory }()
			rbacClientFactory = tt.mockFactory

			// Create command
			cmd := newRoleCreateCommand()
			cmd.SetArgs(tt.args)

			// Capture output
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			// Execute command
			err := cmd.Execute()

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRoleDeleteCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		mockFactory func() (rbac.Client, error)
		expectError bool
		errorMsg    string
	}{
		{
			name: "delete custom role",
			args: []string{"backup-operator", "--yes"},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					DeleteRoleFunc: func(ctx context.Context, req *rbac.DeleteRoleRequest) (*rbac.DeleteRoleResponse, error) {
						return &rbac.DeleteRoleResponse{
							Success: true,
							Message: "Role deleted successfully",
						}, nil
					},
				}, nil
			},
			expectError: false,
		},
		{
			name: "delete built-in role",
			args: []string{"admin", "--yes"},
			mockFactory: func() (rbac.Client, error) {
				t.Fatal("should not call factory")
				return nil, nil
			},
			expectError: true,
			errorMsg:    "cannot delete built-in role",
		},
		{
			name: "delete readonly role",
			args: []string{"readonly", "--yes"},
			mockFactory: func() (rbac.Client, error) {
				t.Fatal("should not call factory")
				return nil, nil
			},
			expectError: true,
			errorMsg:    "cannot delete built-in role",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore factory
			origFactory := rbacClientFactory
			defer func() { rbacClientFactory = origFactory }()
			rbacClientFactory = tt.mockFactory

			// Create command
			cmd := newRoleDeleteCommand()
			cmd.SetArgs(tt.args)

			// Capture output
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			// Execute command
			err := cmd.Execute()

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRoleListCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		mockFactory func() (rbac.Client, error)
		expectError bool
	}{
		{
			name: "list roles as table",
			args: []string{},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					ListRolesFunc: func(ctx context.Context, req *rbac.ListRolesRequest) (*rbac.ListRolesResponse, error) {
						return &rbac.ListRolesResponse{
							Roles: []*rbac.RoleInfo{
								{
									RoleID:      "role-1",
									RoleName:    "admin",
									Permissions: []string{"*:*"},
									CreatedAt:   time.Now().UnixNano(),
								},
								{
									RoleID:      "role-2",
									RoleName:    "readonly",
									Permissions: []string{"keys:read"},
									CreatedAt:   time.Now().UnixNano(),
								},
							},
							TotalCount: 2,
						}, nil
					},
				}, nil
			},
			expectError: false,
		},
		{
			name: "list roles as JSON",
			args: []string{"--output", "json"},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					ListRolesFunc: func(ctx context.Context, req *rbac.ListRolesRequest) (*rbac.ListRolesResponse, error) {
						return &rbac.ListRolesResponse{
							Roles:      []*rbac.RoleInfo{},
							TotalCount: 0,
						}, nil
					},
				}, nil
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore factory
			origFactory := rbacClientFactory
			defer func() { rbacClientFactory = origFactory }()
			rbacClientFactory = tt.mockFactory

			// Save and restore output format
			oldOutputFormat := outputFormat
			defer func() { outputFormat = oldOutputFormat }()

			// Create command
			cmd := newRoleListCommand()
			cmd.SetArgs(tt.args)

			// Capture output
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			// Execute command
			err := cmd.Execute()

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRoleShowCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		mockFactory func() (rbac.Client, error)
		expectError bool
		errorMsg    string
	}{
		{
			name: "show admin role",
			args: []string{"admin"},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					ListRolesFunc: func(ctx context.Context, req *rbac.ListRolesRequest) (*rbac.ListRolesResponse, error) {
						return &rbac.ListRolesResponse{
							Roles: []*rbac.RoleInfo{
								{
									RoleID:      "role-1",
									RoleName:    "admin",
									Permissions: []string{"*:*"},
									CreatedAt:   time.Now().UnixNano(),
								},
							},
							TotalCount: 1,
						}, nil
					},
				}, nil
			},
			expectError: false,
		},
		{
			name: "show role as JSON",
			args: []string{"readonly", "--output", "json"},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					ListRolesFunc: func(ctx context.Context, req *rbac.ListRolesRequest) (*rbac.ListRolesResponse, error) {
						return &rbac.ListRolesResponse{
							Roles: []*rbac.RoleInfo{
								{
									RoleID:      "role-2",
									RoleName:    "readonly",
									Permissions: []string{"keys:read"},
									CreatedAt:   time.Now().UnixNano(),
								},
							},
							TotalCount: 1,
						}, nil
					},
				}, nil
			},
			expectError: false,
		},
		{
			name: "show nonexistent role",
			args: []string{"nonexistent"},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					ListRolesFunc: func(ctx context.Context, req *rbac.ListRolesRequest) (*rbac.ListRolesResponse, error) {
						return &rbac.ListRolesResponse{
							Roles:      []*rbac.RoleInfo{},
							TotalCount: 0,
						}, nil
					},
				}, nil
			},
			expectError: true,
			errorMsg:    "role not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore factory
			origFactory := rbacClientFactory
			defer func() { rbacClientFactory = origFactory }()
			rbacClientFactory = tt.mockFactory

			// Save and restore output format
			oldOutputFormat := outputFormat
			defer func() { outputFormat = oldOutputFormat }()

			// Create command
			cmd := newRoleShowCommand()
			cmd.SetArgs(tt.args)

			// Capture output
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			// Execute command
			err := cmd.Execute()

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRoleAssignCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		mockFactory func() (rbac.Client, error)
		expectError bool
	}{
		{
			name: "assign role to user",
			args: []string{"readwrite", "testuser"},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					AssignRoleFunc: func(ctx context.Context, req *rbac.AssignRoleRequest) (*rbac.AssignRoleResponse, error) {
						return &rbac.AssignRoleResponse{
							Success: true,
							Message: "Role assigned successfully",
						}, nil
					},
				}, nil
			},
			expectError: false,
		},
		{
			name: "assign admin role",
			args: []string{"admin", "adminuser"},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					AssignRoleFunc: func(ctx context.Context, req *rbac.AssignRoleRequest) (*rbac.AssignRoleResponse, error) {
						return &rbac.AssignRoleResponse{
							Success: true,
							Message: "Role assigned successfully",
						}, nil
					},
				}, nil
			},
			expectError: false,
		},
		{
			name: "assign role API error",
			args: []string{"readwrite", "testuser"},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					AssignRoleFunc: func(ctx context.Context, req *rbac.AssignRoleRequest) (*rbac.AssignRoleResponse, error) {
						return nil, errors.Newf(errors.ErrNotFound, "role.assign", "user not found")
					},
				}, nil
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore factory
			origFactory := rbacClientFactory
			defer func() { rbacClientFactory = origFactory }()
			rbacClientFactory = tt.mockFactory

			// Create command
			cmd := newRoleAssignCommand()
			cmd.SetArgs(tt.args)

			// Capture output
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			// Execute command
			err := cmd.Execute()

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestRoleUnassignCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		mockFactory func() (rbac.Client, error)
		expectError bool
	}{
		{
			name: "unassign role from user",
			args: []string{"readwrite", "testuser", "--yes"},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					RevokeRoleFunc: func(ctx context.Context, req *rbac.RevokeRoleRequest) (*rbac.RevokeRoleResponse, error) {
						return &rbac.RevokeRoleResponse{
							Success: true,
							Message: "Role revoked successfully",
						}, nil
					},
				}, nil
			},
			expectError: false,
		},
		{
			name: "unassign role API error",
			args: []string{"readwrite", "testuser", "--yes"},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					RevokeRoleFunc: func(ctx context.Context, req *rbac.RevokeRoleRequest) (*rbac.RevokeRoleResponse, error) {
						return nil, errors.Newf(errors.ErrNotFound, "role.revoke", "user does not have this role")
					},
				}, nil
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore factory
			origFactory := rbacClientFactory
			defer func() { rbacClientFactory = origFactory }()
			rbacClientFactory = tt.mockFactory

			// Save and restore force flag
			oldForce := roleForce
			defer func() { roleForce = oldForce }()
			roleForce = true

			// Create command
			cmd := newRoleUnassignCommand()
			cmd.SetArgs(tt.args)

			// Capture output
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			// Execute command
			err := cmd.Execute()

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatPermissionsForSDK(t *testing.T) {
	tests := []struct {
		name        string
		input       []string
		expectError bool
		errorMsg    string
		expected    []string
	}{
		{
			name:        "single permission",
			input:       []string{"keys:read"},
			expectError: false,
			expected:    []string{"keys:read"},
		},
		{
			name:        "multiple permissions",
			input:       []string{"keys:read", "keys:write"},
			expectError: false,
			expected:    []string{"keys:read", "keys:write"},
		},
		{
			name:        "wildcard resource",
			input:       []string{"*:read"},
			expectError: false,
			expected:    []string{"*:read"},
		},
		{
			name:        "wildcard action",
			input:       []string{"keys:*"},
			expectError: false,
			expected:    []string{"keys:*"},
		},
		{
			name:        "full wildcard",
			input:       []string{"*:*"},
			expectError: false,
			expected:    []string{"*:*"},
		},
		{
			name:        "invalid format - no colon",
			input:       []string{"keysread"},
			expectError: true,
			errorMsg:    "invalid permission format",
		},
		{
			name:        "invalid format - empty resource",
			input:       []string{":read"},
			expectError: true,
			errorMsg:    "invalid permission format",
		},
		{
			name:        "invalid format - empty action",
			input:       []string{"keys:"},
			expectError: true,
			errorMsg:    "invalid permission format",
		},
		{
			name:        "invalid resource name",
			input:       []string{"invalid-resource:read"},
			expectError: true,
			errorMsg:    "invalid resource",
		},
		{
			name:        "invalid action name",
			input:       []string{"keys:invalid-action"},
			expectError: true,
			errorMsg:    "invalid action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := formatPermissionsForSDK(tt.input)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestPermissionConstants(t *testing.T) {
	// Test that auth package constants are accessible
	assert.Equal(t, "admin", auth.RoleNameAdmin)
	assert.Equal(t, "readwrite", auth.RoleNameReadWrite)
	assert.Equal(t, "readonly", auth.RoleNameReadOnly)

	assert.Equal(t, "keys", auth.ResourceKeys)
	assert.Equal(t, "indexes", auth.ResourceIndexes)
	assert.Equal(t, "cluster", auth.ResourceCluster)
	assert.Equal(t, "users", auth.ResourceUsers)
	assert.Equal(t, "backups", auth.ResourceBackups)
	assert.Equal(t, "*", auth.ResourceAll)

	assert.Equal(t, "read", auth.ActionRead)
	assert.Equal(t, "write", auth.ActionWrite)
	assert.Equal(t, "delete", auth.ActionDelete)
	assert.Equal(t, "admin", auth.ActionAdmin)
	assert.Equal(t, "*", auth.ActionAll)
}

// TestRBACClientFactoryError tests error handling when factory returns an error.
func TestRBACClientFactoryError(t *testing.T) {
	// Save original factory
	origFactory := rbacClientFactory
	defer func() { rbacClientFactory = origFactory }()

	rbacClientFactory = func() (rbac.Client, error) {
		return nil, errors.Newf(errors.ErrUnauthorized, "test", "unauthorized")
	}

	tests := []struct {
		name        string
		cmdFunc     func() *cobra.Command
		args        []string
		expectedErr string
	}{
		{
			name:        "list roles - auth error",
			cmdFunc:     newRoleListCommand,
			args:        []string{},
			expectedErr: "unauthorized",
		},
		{
			name:        "show role - auth error",
			cmdFunc:     newRoleShowCommand,
			args:        []string{"admin"},
			expectedErr: "unauthorized",
		},
		{
			name:        "assign role - auth error",
			cmdFunc:     newRoleAssignCommand,
			args:        []string{"admin", "testuser"},
			expectedErr: "unauthorized",
		},
		{
			name:        "unassign role - auth error",
			cmdFunc:     newRoleUnassignCommand,
			args:        []string{"admin", "testuser", "--yes"},
			expectedErr: "unauthorized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore force flag for unassign test
			oldForce := roleForce
			defer func() { roleForce = oldForce }()
			roleForce = true

			cmd := tt.cmdFunc()
			cmd.SetArgs(tt.args)

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

// TestRoleCommandsWithClientErrors tests error propagation from client methods.
func TestRoleCommandsWithClientErrors(t *testing.T) {
	// Save original factory
	origFactory := rbacClientFactory
	defer func() { rbacClientFactory = origFactory }()

	testError := errors.Newf(errors.ErrNotFound, "test", "not found")

	t.Run("list roles - client error", func(t *testing.T) {
		rbacClientFactory = func() (rbac.Client, error) {
			return &MockRBACClient{
				ListRolesFunc: func(ctx context.Context, req *rbac.ListRolesRequest) (*rbac.ListRolesResponse, error) {
					return nil, testError
				},
			}, nil
		}

		cmd := newRoleListCommand()
		cmd.SetArgs([]string{})

		err := cmd.Execute()
		require.Error(t, err)
	})

	t.Run("show role - client error", func(t *testing.T) {
		rbacClientFactory = func() (rbac.Client, error) {
			return &MockRBACClient{
				ListRolesFunc: func(ctx context.Context, req *rbac.ListRolesRequest) (*rbac.ListRolesResponse, error) {
					return nil, testError
				},
			}, nil
		}

		cmd := newRoleShowCommand()
		cmd.SetArgs([]string{"nonexistent"})

		err := cmd.Execute()
		require.Error(t, err)
	})

	t.Run("create role - client error", func(t *testing.T) {
		rbacClientFactory = func() (rbac.Client, error) {
			return &MockRBACClient{
				CreateRoleFunc: func(ctx context.Context, req *rbac.CreateRoleRequest) (*rbac.CreateRoleResponse, error) {
					return nil, errors.Newf(errors.ErrAlreadyExists, "test", "already exists")
				},
			}, nil
		}

		cmd := newRoleCreateCommand()
		cmd.SetArgs([]string{"duplicate-role", "--permissions", "keys:read"})

		err := cmd.Execute()
		require.Error(t, err)
	})

	t.Run("delete role - client error", func(t *testing.T) {
		rbacClientFactory = func() (rbac.Client, error) {
			return &MockRBACClient{
				DeleteRoleFunc: func(ctx context.Context, req *rbac.DeleteRoleRequest) (*rbac.DeleteRoleResponse, error) {
					return nil, testError
				},
			}, nil
		}

		// Save and restore force flag
		oldForce := roleForce
		defer func() { roleForce = oldForce }()
		roleForce = true

		cmd := newRoleDeleteCommand()
		cmd.SetArgs([]string{"nonexistent", "--yes"})

		err := cmd.Execute()
		require.Error(t, err)
	})
}

// TestRoleCommandTimeout tests that commands use proper context timeout.
func TestRoleCommandTimeout(t *testing.T) {
	// Save original factory
	origFactory := rbacClientFactory
	defer func() { rbacClientFactory = origFactory }()

	// Create mock client that checks context
	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			ListRolesFunc: func(ctx context.Context, req *rbac.ListRolesRequest) (*rbac.ListRolesResponse, error) {
				// Verify context has timeout
				deadline, ok := ctx.Deadline()
				assert.True(t, ok, "context should have deadline")
				assert.True(t, deadline.After(time.Now()), "deadline should be in the future")
				return &rbac.ListRolesResponse{
					Roles:      []*rbac.RoleInfo{},
					TotalCount: 0,
				}, nil
			},
		}, nil
	}

	cmd := newRoleListCommand()
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestRoleCommandStructure tests that the role command has correct subcommands.
func TestRoleCommandStructure(t *testing.T) {
	cmd := newRoleCommand()
	assert.Equal(t, "role", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify subcommands exist
	subcommands := cmd.Commands()
	assert.Len(t, subcommands, 6, "should have 6 subcommands: create, delete, list, show, assign, unassign")

	commandNames := make(map[string]bool)
	for _, subcmd := range subcommands {
		commandNames[subcmd.Use] = true
	}

	assert.True(t, commandNames["create <name>"])
	assert.True(t, commandNames["delete <name>"])
	assert.True(t, commandNames["list"])
	assert.True(t, commandNames["show <name>"])
	assert.True(t, commandNames["assign <role> <username>"])
	assert.True(t, commandNames["unassign <role> <username>"])
}

// TestMain initializes the test logger before running tests.
func TestMain(m *testing.M) {
	// Initialize logger for tests
	logConfig := logger.LogConfig{
		Level:     "error", // Reduce noise in tests
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	if err != nil {
		panic(err)
	}
	log = testLog

	// Run tests
	code := m.Run()
	os.Exit(code)
}
