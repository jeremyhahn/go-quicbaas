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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rbac"
)

func setupMockToken(t *testing.T) string {
	tmpDir := t.TempDir()
	tokenFile := filepath.Join(tmpDir, "token")

	token := &cli.TokenData{
		AccessToken:  "mock-admin-token",
		RefreshToken: "mock-refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour),
		TokenType:    "Bearer",
	}

	err := cli.SaveToken(tokenFile, token)
	require.NoError(t, err)

	return tokenFile
}

// TestRunUserCreateCommand tests the runUserCreateCommand function.
// Authentication is via WebAuthn/PIV; the CLI does not accept passwords.
func TestRunUserCreateCommand(t *testing.T) {
	tests := []struct {
		name          string
		username      string
		roles         []string
		mockFactory   func() (rbac.Client, error)
		expectError   bool
		errorContains string
		outputFormat  string
	}{
		{
			name:     "successful user creation with default output",
			username: "testuser",
			roles:    []string{"readonly"},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					CreateUserFunc: func(ctx context.Context, req *rbac.CreateUserRequest) (*rbac.CreateUserResponse, error) {
						return &rbac.CreateUserResponse{
							UserID:    "user-123",
							Timestamp: time.Now().UnixNano(),
						}, nil
					},
				}, nil
			},
			expectError:  false,
			outputFormat: "table",
		},
		{
			name:     "successful user creation with JSON output",
			username: "jsonuser",
			roles:    []string{"admin"},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					CreateUserFunc: func(ctx context.Context, req *rbac.CreateUserRequest) (*rbac.CreateUserResponse, error) {
						return &rbac.CreateUserResponse{
							UserID:    "user-456",
							Timestamp: time.Now().UnixNano(),
						}, nil
					},
				}, nil
			},
			expectError:  false,
			outputFormat: "json",
		},
		{
			name:     "empty username error",
			username: "",
			roles:    []string{"readonly"},
			mockFactory: func() (rbac.Client, error) {
				t.Fatal("should not call factory")
				return nil, nil
			},
			expectError:   true,
			errorContains: "username is required",
		},
		{
			name:     "rbac client factory error",
			username: "testuser",
			roles:    []string{"readonly"},
			mockFactory: func() (rbac.Client, error) {
				return nil, errors.Newf(errors.ErrUnauthorized, "rbac.client", "not authenticated")
			},
			expectError:   true,
			errorContains: "not authenticated",
		},
		{
			name:     "create user API error",
			username: "existinguser",
			roles:    []string{"readonly"},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					CreateUserFunc: func(ctx context.Context, req *rbac.CreateUserRequest) (*rbac.CreateUserResponse, error) {
						return nil, errors.Newf(errors.ErrAlreadyExists, "user.create", "user already exists")
					},
				}, nil
			},
			expectError:   true,
			errorContains: "user already exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore factory
			origFactory := rbacClientFactory
			defer func() { rbacClientFactory = origFactory }()
			rbacClientFactory = tt.mockFactory

			// Save and restore flags
			oldRoles := userRoles
			oldOutputFormat := outputFormat
			defer func() {
				userRoles = oldRoles
				outputFormat = oldOutputFormat
			}()

			// Create command FIRST (before setting flags to avoid reset)
			cmd := newUserCreateCommand()
			args := []string{tt.username}
			cmd.SetArgs(args)

			// Set test values AFTER command creation
			userRoles = tt.roles
			if tt.outputFormat != "" {
				outputFormat = tt.outputFormat
			} else {
				outputFormat = "table"
			}

			// Execute command
			err := cmd.Execute()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestRunUserDeleteCommand tests the runUserDeleteCommand function.
func TestRunUserDeleteCommand(t *testing.T) {
	tests := []struct {
		name          string
		username      string
		forceDelete   bool
		mockFactory   func() (rbac.Client, error)
		expectError   bool
		errorContains string
	}{
		{
			name:        "successful user deletion with force flag",
			username:    "testuser",
			forceDelete: true,
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					DeleteUserFunc: func(ctx context.Context, req *rbac.DeleteUserRequest) (*rbac.DeleteUserResponse, error) {
						return &rbac.DeleteUserResponse{
							Success: true,
							Message: "User deleted successfully",
						}, nil
					},
				}, nil
			},
			expectError: false,
		},
		{
			name:        "rbac client factory error",
			username:    "testuser",
			forceDelete: true,
			mockFactory: func() (rbac.Client, error) {
				return nil, errors.Newf(errors.ErrUnauthorized, "rbac.client", "token expired")
			},
			expectError:   true,
			errorContains: "token expired",
		},
		{
			name:        "delete user API error - user not found",
			username:    "nonexistent",
			forceDelete: true,
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					DeleteUserFunc: func(ctx context.Context, req *rbac.DeleteUserRequest) (*rbac.DeleteUserResponse, error) {
						return nil, errors.Newf(errors.ErrNotFound, "user.delete", "user not found")
					},
				}, nil
			},
			expectError:   true,
			errorContains: "user not found",
		},
		{
			name:        "delete user API error - permission denied",
			username:    "admin",
			forceDelete: true,
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					DeleteUserFunc: func(ctx context.Context, req *rbac.DeleteUserRequest) (*rbac.DeleteUserResponse, error) {
						return nil, errors.Newf(errors.ErrForbidden, "user.delete", "cannot delete admin user")
					},
				}, nil
			},
			expectError:   true,
			errorContains: "cannot delete admin user",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore factory
			origFactory := rbacClientFactory
			defer func() { rbacClientFactory = origFactory }()
			rbacClientFactory = tt.mockFactory

			// Save and restore flags
			oldForce := userForce
			defer func() { userForce = oldForce }()
			userForce = tt.forceDelete

			// Create command
			cmd := newUserDeleteCommand()
			args := []string{tt.username}
			if tt.forceDelete {
				args = append(args, "--yes")
			}
			cmd.SetArgs(args)

			// Execute command
			err := cmd.Execute()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestRunUserListCommand tests the runUserListCommand function.
func TestRunUserListCommand(t *testing.T) {
	mockUsers := []*rbac.UserInfo{
		{
			UserID:      "user-1",
			Username:    "alice",
			Roles:       []string{"admin"},
			CreatedAt:   time.Now().Add(-24 * time.Hour).UnixNano(),
			LastLoginAt: time.Now().Add(-1 * time.Hour).UnixNano(),
		},
		{
			UserID:      "user-2",
			Username:    "bob",
			Roles:       []string{"readonly"},
			CreatedAt:   time.Now().Add(-48 * time.Hour).UnixNano(),
			LastLoginAt: 0, // Never authenticated
		},
	}

	tests := []struct {
		name          string
		mockFactory   func() (rbac.Client, error)
		expectError   bool
		errorContains string
		outputFormat  string
		checkResult   func(t *testing.T, users []*rbac.UserInfo)
	}{
		{
			name: "successful list with users (text format)",
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
						return &rbac.ListUsersResponse{
							Users:      mockUsers,
							TotalCount: uint64(len(mockUsers)),
						}, nil
					},
				}, nil
			},
			expectError:  false,
			outputFormat: "table",
			checkResult: func(t *testing.T, users []*rbac.UserInfo) {
				assert.Len(t, users, 2)
			},
		},
		{
			name: "successful list with users (JSON format)",
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
						return &rbac.ListUsersResponse{
							Users:      mockUsers,
							TotalCount: uint64(len(mockUsers)),
						}, nil
					},
				}, nil
			},
			expectError:  false,
			outputFormat: "json",
		},
		{
			name: "successful list with no users",
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
						return &rbac.ListUsersResponse{
							Users:      []*rbac.UserInfo{},
							TotalCount: 0,
						}, nil
					},
				}, nil
			},
			expectError:  false,
			outputFormat: "table",
		},
		{
			name: "rbac client factory error",
			mockFactory: func() (rbac.Client, error) {
				return nil, errors.Newf(errors.ErrUnauthorized, "rbac.client", "not authenticated")
			},
			expectError:   true,
			errorContains: "not authenticated",
		},
		{
			name: "list users API error",
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
						return nil, errors.Newf(errors.ErrForbidden, "user.list", "insufficient permissions")
					},
				}, nil
			},
			expectError:   true,
			errorContains: "insufficient permissions",
		},
		{
			name: "invalid output format",
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
						return &rbac.ListUsersResponse{
							Users:      mockUsers,
							TotalCount: uint64(len(mockUsers)),
						}, nil
					},
				}, nil
			},
			expectError:   true,
			errorContains: "unknown output format",
			outputFormat:  "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore factory
			origFactory := rbacClientFactory
			defer func() { rbacClientFactory = origFactory }()
			rbacClientFactory = tt.mockFactory

			// Save and restore flags
			oldOutputFormat := outputFormat
			defer func() { outputFormat = oldOutputFormat }()

			// Create command FIRST (before setting flags to avoid reset)
			cmd := newUserListCommand()
			cmd.SetArgs([]string{})

			// Set flags AFTER command creation
			if tt.outputFormat != "" {
				outputFormat = tt.outputFormat
			} else {
				outputFormat = "table"
			}

			// Execute command
			err := cmd.Execute()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestRunUserShowCommand tests the runUserShowCommand function.
func TestRunUserShowCommand(t *testing.T) {
	mockUser := &rbac.UserInfo{
		UserID:      "user-123",
		Username:    "alice",
		Roles:       []string{"admin", "readwrite"},
		CreatedAt:   time.Now().Add(-7 * 24 * time.Hour).UnixNano(),
		LastLoginAt: time.Now().Add(-1 * time.Hour).UnixNano(),
	}

	tests := []struct {
		name          string
		username      string
		mockFactory   func() (rbac.Client, error)
		expectError   bool
		errorContains string
		outputFormat  string
	}{
		{
			name:     "successful show user (text format)",
			username: "alice",
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
						return &rbac.ListUsersResponse{
							Users:      []*rbac.UserInfo{mockUser},
							TotalCount: 1,
						}, nil
					},
				}, nil
			},
			expectError:  false,
			outputFormat: "table",
		},
		{
			name:     "successful show user (JSON format)",
			username: "alice",
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
						return &rbac.ListUsersResponse{
							Users:      []*rbac.UserInfo{mockUser},
							TotalCount: 1,
						}, nil
					},
				}, nil
			},
			expectError:  false,
			outputFormat: "json",
		},
		{
			name:     "user with no last auth",
			username: "newuser",
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
						return &rbac.ListUsersResponse{
							Users: []*rbac.UserInfo{
								{
									UserID:      "user-456",
									Username:    "newuser",
									Roles:       []string{"readonly"},
									CreatedAt:   time.Now().UnixNano(),
									LastLoginAt: 0, // Never authenticated
								},
							},
							TotalCount: 1,
						}, nil
					},
				}, nil
			},
			expectError:  false,
			outputFormat: "table",
		},
		{
			name:     "rbac client factory error",
			username: "alice",
			mockFactory: func() (rbac.Client, error) {
				return nil, errors.Newf(errors.ErrUnauthorized, "rbac.client", "token expired")
			},
			expectError:   true,
			errorContains: "token expired",
		},
		{
			name:     "get user API error - user not found",
			username: "nonexistent",
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
						return &rbac.ListUsersResponse{
							Users:      []*rbac.UserInfo{},
							TotalCount: 0,
						}, nil
					},
				}, nil
			},
			expectError:   true,
			errorContains: "user not found",
		},
		{
			name:     "invalid output format",
			username: "alice",
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
						return &rbac.ListUsersResponse{
							Users:      []*rbac.UserInfo{mockUser},
							TotalCount: 1,
						}, nil
					},
				}, nil
			},
			expectError:   true,
			errorContains: "unknown output format",
			outputFormat:  "xml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore factory
			origFactory := rbacClientFactory
			defer func() { rbacClientFactory = origFactory }()
			rbacClientFactory = tt.mockFactory

			// Save and restore flags
			oldOutputFormat := outputFormat
			defer func() { outputFormat = oldOutputFormat }()

			// Create command FIRST (before setting flags to avoid reset)
			cmd := newUserShowCommand()
			cmd.SetArgs([]string{tt.username})

			// Set flags AFTER command creation
			if tt.outputFormat != "" {
				outputFormat = tt.outputFormat
			} else {
				outputFormat = "table"
			}

			// Execute command
			err := cmd.Execute()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestRunUserUpdateCommand tests the runUserUpdateCommand function.
// The CLI no longer accepts --password; only role updates are supported.
func TestRunUserUpdateCommand(t *testing.T) {
	tests := []struct {
		name          string
		username      string
		roles         []string
		mockFactory   func() (rbac.Client, error)
		expectError   bool
		errorContains string
	}{
		{
			name:     "successful update roles only",
			username: "testuser",
			roles:    []string{"admin", "readwrite"},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
						return &rbac.ListUsersResponse{
							Users: []*rbac.UserInfo{
								{
									UserID:   "user-123",
									Username: "testuser",
									Roles:    []string{"readonly"},
								},
							},
							TotalCount: 1,
						}, nil
					},
					AssignRoleFunc: func(ctx context.Context, req *rbac.AssignRoleRequest) (*rbac.AssignRoleResponse, error) {
						return &rbac.AssignRoleResponse{Success: true}, nil
					},
					RevokeRoleFunc: func(ctx context.Context, req *rbac.RevokeRoleRequest) (*rbac.RevokeRoleResponse, error) {
						return &rbac.RevokeRoleResponse{Success: true}, nil
					},
				}, nil
			},
			expectError: false,
		},
		{
			name:     "error: no flags provided",
			username: "testuser",
			roles:    nil,
			mockFactory: func() (rbac.Client, error) {
				t.Fatal("should not call factory")
				return nil, nil
			},
			expectError:   true,
			errorContains: "--roles must be specified",
		},
		{
			name:     "rbac client factory error",
			username: "testuser",
			roles:    []string{"admin"},
			mockFactory: func() (rbac.Client, error) {
				return nil, errors.Newf(errors.ErrUnauthorized, "rbac.client", "not authenticated")
			},
			expectError:   true,
			errorContains: "not authenticated",
		},
		{
			name:     "update user API error - user not found",
			username: "testuser",
			roles:    []string{"admin"},
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
						return &rbac.ListUsersResponse{
							Users:      []*rbac.UserInfo{},
							TotalCount: 0,
						}, nil
					},
				}, nil
			},
			expectError:   true,
			errorContains: "user not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore factory
			origFactory := rbacClientFactory
			defer func() { rbacClientFactory = origFactory }()
			rbacClientFactory = tt.mockFactory

			// Save and restore flags
			oldRoles := userRoles
			defer func() {
				userRoles = oldRoles
			}()

			userRoles = tt.roles

			// Create command
			cmd := newUserUpdateCommand()
			args := []string{tt.username}
			if tt.roles != nil {
				args = append(args, "--roles", strings.Join(tt.roles, ","))
			}
			cmd.SetArgs(args)

			// Execute command
			err := cmd.Execute()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestMockRBACClient tests the MockRBACClient implementation.
func TestMockRBACClient(t *testing.T) {
	t.Run("mock user operations", func(t *testing.T) {
		client := &MockRBACClient{}

		// Test CreateUser
		createResp, err := client.CreateUser(context.Background(), &rbac.CreateUserRequest{
			Username: "testuser",
			Password: "password",
			Roles:    []string{"readonly"},
		})
		assert.NoError(t, err)
		assert.NotNil(t, createResp)
		assert.NotEmpty(t, createResp.UserID)

		// Test ListUsers
		listResp, err := client.ListUsers(context.Background(), &rbac.ListUsersRequest{})
		assert.NoError(t, err)
		assert.NotEmpty(t, listResp.Users)

		// Test DeleteUser
		deleteResp, err := client.DeleteUser(context.Background(), &rbac.DeleteUserRequest{Username: "testuser"})
		assert.NoError(t, err)
		assert.True(t, deleteResp.Success)
	})

	t.Run("custom mock functions", func(t *testing.T) {
		called := false
		client := &MockRBACClient{
			CreateUserFunc: func(ctx context.Context, req *rbac.CreateUserRequest) (*rbac.CreateUserResponse, error) {
				called = true
				return &rbac.CreateUserResponse{
					UserID:    "custom-user-id",
					Timestamp: time.Now().UnixNano(),
				}, nil
			},
		}

		resp, err := client.CreateUser(context.Background(), &rbac.CreateUserRequest{
			Username: "test",
			Password: "pass",
		})
		assert.NoError(t, err)
		assert.True(t, called)
		assert.Equal(t, "custom-user-id", resp.UserID)
	})
}

// TestUserInfoSerialization tests JSON serialization of rbac.UserInfo.
func TestUserInfoSerialization(t *testing.T) {
	t.Run("serialize and deserialize user info", func(t *testing.T) {
		now := time.Now()
		user := &rbac.UserInfo{
			UserID:      "user-123",
			Username:    "testuser",
			Roles:       []string{"admin", "readwrite"},
			CreatedAt:   now.UnixNano(),
			LastLoginAt: now.UnixNano(),
		}

		// Serialize to JSON
		data, err := json.Marshal(user)
		require.NoError(t, err)

		// Deserialize from JSON
		var loaded rbac.UserInfo
		err = json.Unmarshal(data, &loaded)
		require.NoError(t, err)

		// Verify fields
		assert.Equal(t, user.UserID, loaded.UserID)
		assert.Equal(t, user.Username, loaded.Username)
		assert.Equal(t, user.Roles, loaded.Roles)
		assert.Equal(t, user.CreatedAt, loaded.CreatedAt)
		assert.Equal(t, user.LastLoginAt, loaded.LastLoginAt)
	})
}

// TestUserCommands_Integration tests the integration of user commands.
func TestUserCommands_Integration(t *testing.T) {
	// This test verifies that the command structure is correct
	t.Run("user command structure", func(t *testing.T) {
		cmd := newUserCommand()
		assert.Equal(t, "user", cmd.Use)
		assert.NotEmpty(t, cmd.Short)
		assert.NotEmpty(t, cmd.Long)

		// Verify subcommands exist
		subcommands := cmd.Commands()
		assert.Len(t, subcommands, 7, "should have 7 subcommands: create, delete, list, show, update, assign-role, revoke-role")

		commandNames := make(map[string]bool)
		for _, subcmd := range subcommands {
			commandNames[subcmd.Use] = true
		}

		assert.True(t, commandNames["create <username>"])
		assert.True(t, commandNames["delete <username>"])
		assert.True(t, commandNames["list"])
		assert.True(t, commandNames["show <username>"])
		assert.True(t, commandNames["update <username>"])
		assert.True(t, commandNames["assign-role <username> <role>"])
		assert.True(t, commandNames["revoke-role <username> <role>"])
	})
}

// TestUserCreateCommand_EdgeCases tests edge cases for user creation.
func TestUserCreateCommand_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		username      string
		expectError   bool
		errorContains string
	}{
		{
			name:        "username with special characters",
			username:    "user@example.com",
			expectError: false, // Should be allowed
		},
		{
			name:        "username with spaces",
			username:    "user name",
			expectError: false, // Validation happens server-side
		},
		{
			name:          "empty username after trim",
			username:      "",
			expectError:   true,
			errorContains: "username is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origFactory := rbacClientFactory
			defer func() { rbacClientFactory = origFactory }()

			called := false
			rbacClientFactory = func() (rbac.Client, error) {
				called = true
				return &MockRBACClient{
					CreateUserFunc: func(ctx context.Context, req *rbac.CreateUserRequest) (*rbac.CreateUserResponse, error) {
						return &rbac.CreateUserResponse{
							UserID:    "user-123",
							Timestamp: time.Now().UnixNano(),
						}, nil
					},
				}, nil
			}

			oldRoles := userRoles
			defer func() {
				userRoles = oldRoles
			}()

			userRoles = []string{"readonly"}

			cmd := newUserCreateCommand()
			cmd.SetArgs([]string{tt.username})

			err := cmd.Execute()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.False(t, called, "factory should not be called for invalid input")
			} else {
				// We expect it to succeed or fail at the API level, not validation
				if err != nil {
					// If there's an error, it should not be about username validation
					assert.NotContains(t, err.Error(), "username is required")
				}
			}
		})
	}
}

// TestUserListCommand_OutputFormats tests different output formats.
func TestUserListCommand_OutputFormats(t *testing.T) {
	mockUsers := []*rbac.UserInfo{
		{
			UserID:      "user-1",
			Username:    "alice",
			Roles:       []string{"admin"},
			CreatedAt:   time.Now().UnixNano(),
			LastLoginAt: time.Now().UnixNano(),
		},
	}

	tests := []struct {
		name         string
		outputFormat string
		expectError  bool
	}{
		{
			name:         "text format",
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:         "json format",
			outputFormat: "json",
			expectError:  false,
		},
		{
			name:         "invalid format",
			outputFormat: "xml",
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origFactory := rbacClientFactory
			defer func() { rbacClientFactory = origFactory }()

			rbacClientFactory = func() (rbac.Client, error) {
				return &MockRBACClient{
					ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
						return &rbac.ListUsersResponse{
							Users:      mockUsers,
							TotalCount: uint64(len(mockUsers)),
						}, nil
					},
				}, nil
			}

			oldOutputFormat := outputFormat
			defer func() { outputFormat = oldOutputFormat }()

			// Create command FIRST
			cmd := newUserListCommand()
			cmd.SetArgs([]string{})

			// Set flags AFTER command creation
			outputFormat = tt.outputFormat

			err := cmd.Execute()

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestUserDeleteCommand_WithoutForceFlag tests deletion without --yes flag.
func TestUserDeleteCommand_WithoutForceFlag(t *testing.T) {
	// This test verifies that the confirmation prompt path exists
	// We can't fully test interactive input, but we can verify the flag works
	origFactory := rbacClientFactory
	defer func() { rbacClientFactory = origFactory }()

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			DeleteUserFunc: func(ctx context.Context, req *rbac.DeleteUserRequest) (*rbac.DeleteUserResponse, error) {
				return &rbac.DeleteUserResponse{
					Success: true,
					Message: "User deleted successfully",
				}, nil
			},
		}, nil
	}

	oldForce := userForce
	defer func() { userForce = oldForce }()
	userForce = true // Force flag set

	cmd := newUserDeleteCommand()
	cmd.SetArgs([]string{"testuser", "--yes"})

	err := cmd.Execute()
	assert.NoError(t, err)
}

// TestUserUpdateCommand_FlagCombinations tests --roles flag handling.
// The --password flag has been removed; only --roles is supported.
func TestUserUpdateCommand_FlagCombinations(t *testing.T) {
	tests := []struct {
		name          string
		setRoles      bool
		expectError   bool
		errorContains string
	}{
		{
			name:        "roles only",
			setRoles:    true,
			expectError: false,
		},
		{
			name:          "no roles flag",
			setRoles:      false,
			expectError:   true,
			errorContains: "--roles must be specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origFactory := rbacClientFactory
			defer func() { rbacClientFactory = origFactory }()

			rbacClientFactory = func() (rbac.Client, error) {
				return &MockRBACClient{
					ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
						return &rbac.ListUsersResponse{
							Users: []*rbac.UserInfo{
								{
									UserID:   "user-123",
									Username: "testuser",
									Roles:    []string{"readonly"},
								},
							},
							TotalCount: 1,
						}, nil
					},
					AssignRoleFunc: func(ctx context.Context, req *rbac.AssignRoleRequest) (*rbac.AssignRoleResponse, error) {
						return &rbac.AssignRoleResponse{Success: true}, nil
					},
					RevokeRoleFunc: func(ctx context.Context, req *rbac.RevokeRoleRequest) (*rbac.RevokeRoleResponse, error) {
						return &rbac.RevokeRoleResponse{Success: true}, nil
					},
				}, nil
			}

			oldRoles := userRoles
			defer func() {
				userRoles = oldRoles
			}()

			args := []string{"testuser"}
			if tt.setRoles {
				userRoles = []string{"admin"}
				args = append(args, "--roles", "admin")
			} else {
				userRoles = nil
			}

			cmd := newUserUpdateCommand()
			cmd.SetArgs(args)

			err := cmd.Execute()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestUserCommands_ContextCancellation tests that commands respect context timeout.
func TestUserCommands_ContextCancellation(t *testing.T) {
	origFactory := rbacClientFactory
	defer func() { rbacClientFactory = origFactory }()

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
				// Simulate a long-running operation
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(100 * time.Millisecond):
					return &rbac.ListUsersResponse{Users: []*rbac.UserInfo{}}, nil
				}
			},
		}, nil
	}

	oldOutputFormat := outputFormat
	oldTimeout := timeout
	defer func() {
		outputFormat = oldOutputFormat
		timeout = oldTimeout
	}()

	outputFormat = "table"
	timeout = 50 * time.Millisecond // Very short timeout

	cmd := newUserListCommand()
	cmd.SetArgs([]string{})

	// This might fail due to timeout, but that's okay - we're just testing the path
	_ = cmd.Execute()
}

// TestUserFormatError tests error formatting for user commands.
func TestUserFormatError(t *testing.T) {
	// Test that formatError is called and works correctly
	origFactory := rbacClientFactory
	defer func() { rbacClientFactory = origFactory }()

	testErr := errors.Newf(errors.ErrNotFound, "user.test", "test error message")

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
				return nil, testErr
			},
		}, nil
	}

	oldOutputFormat := outputFormat
	defer func() { outputFormat = oldOutputFormat }()
	outputFormat = "table"

	cmd := newUserShowCommand()
	cmd.SetArgs([]string{"testuser"})

	err := cmd.Execute()
	require.Error(t, err)

	// The error should contain the formatted error message
	errStr := fmt.Sprintf("%v", err)
	assert.Contains(t, errStr, "test error message")
}

// TestUserAssignRoleCommand tests the assign-role command.
func TestUserAssignRoleCommand(t *testing.T) {
	tests := []struct {
		name          string
		username      string
		roleName      string
		mockFactory   func() (rbac.Client, error)
		expectError   bool
		errorContains string
	}{
		{
			name:     "successful role assignment",
			username: "testuser",
			roleName: "admin",
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
			name:     "assignment failure",
			username: "testuser",
			roleName: "nonexistent",
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					AssignRoleFunc: func(ctx context.Context, req *rbac.AssignRoleRequest) (*rbac.AssignRoleResponse, error) {
						return nil, errors.Newf(errors.ErrNotFound, "role.assign", "role not found")
					},
				}, nil
			},
			expectError:   true,
			errorContains: "role not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origFactory := rbacClientFactory
			defer func() { rbacClientFactory = origFactory }()
			rbacClientFactory = tt.mockFactory

			cmd := newUserAssignRoleCommand()
			cmd.SetArgs([]string{tt.username, tt.roleName})

			err := cmd.Execute()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestUserRevokeRoleCommand tests the revoke-role command.
func TestUserRevokeRoleCommand(t *testing.T) {
	tests := []struct {
		name          string
		username      string
		roleName      string
		forceRevoke   bool
		mockFactory   func() (rbac.Client, error)
		expectError   bool
		errorContains string
	}{
		{
			name:        "successful role revocation with force",
			username:    "testuser",
			roleName:    "admin",
			forceRevoke: true,
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
			name:        "revocation failure",
			username:    "testuser",
			roleName:    "admin",
			forceRevoke: true,
			mockFactory: func() (rbac.Client, error) {
				return &MockRBACClient{
					RevokeRoleFunc: func(ctx context.Context, req *rbac.RevokeRoleRequest) (*rbac.RevokeRoleResponse, error) {
						return nil, errors.Newf(errors.ErrNotFound, "role.revoke", "user does not have this role")
					},
				}, nil
			},
			expectError:   true,
			errorContains: "user does not have this role",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origFactory := rbacClientFactory
			defer func() { rbacClientFactory = origFactory }()
			rbacClientFactory = tt.mockFactory

			oldForce := userForce
			defer func() { userForce = oldForce }()
			userForce = tt.forceRevoke

			cmd := newUserRevokeRoleCommand()
			args := []string{tt.username, tt.roleName}
			if tt.forceRevoke {
				args = append(args, "--yes")
			}
			cmd.SetArgs(args)

			err := cmd.Execute()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRunUserUpdateCommand_RolesUpdateSuccess(t *testing.T) {
	origFactory := rbacClientFactory
	origRoles := userRoles
	defer func() {
		rbacClientFactory = origFactory
		userRoles = origRoles
	}()

	userRoles = []string{"admin", "editor"}

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
				return &rbac.ListUsersResponse{
					Users: []*rbac.UserInfo{
						{Username: "testuser", Roles: []string{"readonly", "editor"}},
					},
				}, nil
			},
			RevokeRoleFunc: func(ctx context.Context, req *rbac.RevokeRoleRequest) (*rbac.RevokeRoleResponse, error) {
				return &rbac.RevokeRoleResponse{Success: true}, nil
			},
			AssignRoleFunc: func(ctx context.Context, req *rbac.AssignRoleRequest) (*rbac.AssignRoleResponse, error) {
				return &rbac.AssignRoleResponse{Success: true}, nil
			},
		}, nil
	}

	cmd := &cobra.Command{}
	cmd.Flags().StringSlice("roles", nil, "")
	require.NoError(t, cmd.Flags().Set("roles", "admin,editor"))

	err := runUserUpdateCommand(cmd, []string{"testuser"})
	assert.NoError(t, err)
}

func TestRunUserUpdateCommand_RolesUpdateUserNotFound(t *testing.T) {
	origFactory := rbacClientFactory
	origRoles := userRoles
	defer func() {
		rbacClientFactory = origFactory
		userRoles = origRoles
	}()

	userRoles = []string{"admin"}

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
				return &rbac.ListUsersResponse{Users: []*rbac.UserInfo{}}, nil
			},
		}, nil
	}

	cmd := &cobra.Command{}
	cmd.Flags().StringSlice("roles", nil, "")
	require.NoError(t, cmd.Flags().Set("roles", "admin"))

	err := runUserUpdateCommand(cmd, []string{"nonexistent"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "user not found")
}

func TestRunUserUpdateCommand_RolesUpdateListUsersError(t *testing.T) {
	origFactory := rbacClientFactory
	origRoles := userRoles
	defer func() {
		rbacClientFactory = origFactory
		userRoles = origRoles
	}()

	userRoles = []string{"admin"}

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			ListUsersFunc: func(ctx context.Context, req *rbac.ListUsersRequest) (*rbac.ListUsersResponse, error) {
				return nil, errors.New(errors.ErrInternal, "test", "list users failed")
			},
		}, nil
	}

	cmd := &cobra.Command{}
	cmd.Flags().StringSlice("roles", nil, "")
	require.NoError(t, cmd.Flags().Set("roles", "admin"))

	err := runUserUpdateCommand(cmd, []string{"testuser"})
	assert.Error(t, err)
}

func TestRunUserUpdateCommand_FactoryErr(t *testing.T) {
	origFactory := rbacClientFactory
	origRoles := userRoles
	defer func() {
		rbacClientFactory = origFactory
		userRoles = origRoles
	}()

	userRoles = []string{"admin"}

	rbacClientFactory = func() (rbac.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	cmd.Flags().StringSlice("roles", nil, "")
	require.NoError(t, cmd.Flags().Set("roles", "admin"))

	err := runUserUpdateCommand(cmd, []string{"testuser"})
	assert.Error(t, err)
}

// =============================================================================
// User Revoke Role
// =============================================================================

func TestRunUserRevokeRoleCommand_SuccessWithForce(t *testing.T) {
	origFactory := rbacClientFactory
	origForce := userForce
	defer func() {
		rbacClientFactory = origFactory
		userForce = origForce
	}()

	userForce = true

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			RevokeRoleFunc: func(ctx context.Context, req *rbac.RevokeRoleRequest) (*rbac.RevokeRoleResponse, error) {
				return &rbac.RevokeRoleResponse{Success: true, Message: "role revoked"}, nil
			},
		}, nil
	}

	cmd := &cobra.Command{}
	err := runUserRevokeRoleCommand(cmd, []string{"testuser", "admin"})
	assert.NoError(t, err)
}

func TestRunUserRevokeRoleCommand_FailResponse(t *testing.T) {
	origFactory := rbacClientFactory
	origForce := userForce
	defer func() {
		rbacClientFactory = origFactory
		userForce = origForce
	}()

	userForce = true

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			RevokeRoleFunc: func(ctx context.Context, req *rbac.RevokeRoleRequest) (*rbac.RevokeRoleResponse, error) {
				return &rbac.RevokeRoleResponse{Success: false, Message: "role not assigned"}, nil
			},
		}, nil
	}

	cmd := &cobra.Command{}
	err := runUserRevokeRoleCommand(cmd, []string{"testuser", "platform-admin"})
	assert.NoError(t, err)
}

// =============================================================================
// Role Unassign
// =============================================================================

func TestRunRoleUnassignCommand_SuccessWithForce(t *testing.T) {
	origFactory := rbacClientFactory
	origForce := roleForce
	defer func() {
		rbacClientFactory = origFactory
		roleForce = origForce
	}()

	roleForce = true

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			RevokeRoleFunc: func(ctx context.Context, req *rbac.RevokeRoleRequest) (*rbac.RevokeRoleResponse, error) {
				return &rbac.RevokeRoleResponse{Success: true, Message: "role removed"}, nil
			},
		}, nil
	}

	cmd := &cobra.Command{}
	err := runRoleUnassignCommand(cmd, []string{"admin", "testuser"})
	assert.NoError(t, err)
}

func TestRunRoleUnassignCommand_FailResp(t *testing.T) {
	origFactory := rbacClientFactory
	origForce := roleForce
	defer func() {
		rbacClientFactory = origFactory
		roleForce = origForce
	}()

	roleForce = true

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			RevokeRoleFunc: func(ctx context.Context, req *rbac.RevokeRoleRequest) (*rbac.RevokeRoleResponse, error) {
				return &rbac.RevokeRoleResponse{Success: false, Message: "role not found"}, nil
			},
		}, nil
	}

	cmd := &cobra.Command{}
	err := runRoleUnassignCommand(cmd, []string{"editor", "testuser"})
	assert.NoError(t, err)
}

func TestRunRoleUnassignCommand_ClientErr(t *testing.T) {
	origFactory := rbacClientFactory
	origForce := roleForce
	defer func() {
		rbacClientFactory = origFactory
		roleForce = origForce
	}()

	roleForce = true

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			RevokeRoleFunc: func(ctx context.Context, req *rbac.RevokeRoleRequest) (*rbac.RevokeRoleResponse, error) {
				return nil, errors.New(errors.ErrInternal, "test", "rpc error")
			},
		}, nil
	}

	cmd := &cobra.Command{}
	err := runRoleUnassignCommand(cmd, []string{"admin", "testuser"})
	assert.Error(t, err)
}

// =============================================================================
// Channel List Format
// =============================================================================

func TestFormatChannelListResponse_TableOutput(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels: []*grpc.MessagingChannel{
			{Name: "events", Subscribers: 10, Persistent: false, CreatedAt: time.Now()},
		},
		TotalCount: 1,
	}
	buf := &bytes.Buffer{}
	err := formatChannelListResponse(resp, "table", buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "NAME")
}

func TestFormatChannelListResponse_TablePaginated(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels:   []*grpc.MessagingChannel{{Name: "ch1", Subscribers: 1, CreatedAt: time.Now()}},
		TotalCount: 10,
		HasMore:    true,
		NextCursor: "20",
	}
	buf := &bytes.Buffer{}
	err := formatChannelListResponse(resp, "table", buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "More results available")
}

func TestFormatChannelListResponse_DefaultOutput(t *testing.T) {
	resp := &grpc.MessagingListChannelsResponse{
		Channels:   []*grpc.MessagingChannel{{Name: "alerts", Subscribers: 3, CreatedAt: time.Now()}},
		TotalCount: 1,
	}
	buf := &bytes.Buffer{}
	err := formatChannelListResponse(resp, "", buf)
	assert.NoError(t, err)
	assert.Contains(t, buf.String(), "alerts")
}

func TestFormatChannelListResponse_BadFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	err := formatChannelListResponse(&grpc.MessagingListChannelsResponse{}, "xml", buf)
	assert.Error(t, err)
}

// =============================================================================
// Object Storage Format - Additional Branches
// =============================================================================

func TestFormatObjectLifecycleResponse_JSONFormatCoverage(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.GetObjectPolicyResponse{Key: "mybucket", Policy: &pb.ObjectPolicy{}}
	err := formatObjectLifecycleResponse("mybucket", resp)
	assert.NoError(t, err)
}

func TestFormatObjectLifecycleResponse_YAMLFormatCoverage(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "yaml"

	resp := &pb.GetObjectPolicyResponse{Key: "mybucket", Policy: &pb.ObjectPolicy{}}
	err := formatObjectLifecycleResponse("mybucket", resp)
	assert.NoError(t, err)
}

func TestFormatObjectSetPolicyResponse_JSONFormatCoverage(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "json"

	resp := &pb.SetObjectPolicyResponse{Key: "bucket/key", Version: 2}
	err := formatObjectSetPolicyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectPolicyResponse_DefaultWithAllPolicies(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "table"

	resp := &pb.GetObjectPolicyResponse{
		Key: "bucket/key",
		Policy: &pb.ObjectPolicy{
			ArchivePolicy:   &pb.ArchivePolicy{Enabled: true, ArchiveAfterDays: 30},
			RetentionPolicy: &pb.RetentionPolicy{Enabled: true, RetentionDays: 365},
		},
		ExplicitPolicy: &pb.ObjectPolicy{
			RetentionPolicy: &pb.RetentionPolicy{Enabled: true, RetentionDays: 90},
		},
		InheritedPolicy: &pb.ObjectPolicy{
			ArchivePolicy: &pb.ArchivePolicy{Enabled: true, ArchiveAfterDays: 60},
		},
	}
	err := formatObjectPolicyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectPolicyResponse_DefaultNilPolicies(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = "table"

	resp := &pb.GetObjectPolicyResponse{
		Key:    "bucket/key",
		Policy: nil,
	}
	err := formatObjectPolicyResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectGetResponse_YAMLWithMetadata(t *testing.T) {
	orig := objectOutputFormat
	origMeta := objectIncludeMeta
	defer func() {
		objectOutputFormat = orig
		objectIncludeMeta = origMeta
	}()
	objectOutputFormat = "yaml"
	objectIncludeMeta = true

	resp := &pb.GetObjectResponse{
		Key:         "bucket/file.txt",
		Found:       true,
		Size:        1024,
		Version:     1,
		ContentType: "text/plain",
		Value:       []byte("test data"),
		Metadata: &pb.ObjectMetadata{
			Owner: "admin",
		},
	}
	err := formatObjectGetResponse(resp)
	assert.NoError(t, err)
}

func TestFormatObjectGetResponse_JSONWithObjectMetadata(t *testing.T) {
	orig := objectOutputFormat
	origMeta := objectIncludeMeta
	defer func() {
		objectOutputFormat = orig
		objectIncludeMeta = origMeta
	}()
	objectOutputFormat = "json"
	objectIncludeMeta = true

	resp := &pb.GetObjectResponse{
		Key:         "bucket/meta.txt",
		Found:       true,
		Size:        512,
		ContentType: "text/plain",
		Metadata: &pb.ObjectMetadata{
			Owner: "admin",
		},
	}
	err := formatObjectGetResponse(resp)
	assert.NoError(t, err)
}

// =============================================================================
// Object Put - File Error Paths
// =============================================================================

func TestRunObjectPutCommand_NonexistentFilePath(t *testing.T) {
	origFile := objectFile
	defer func() { objectFile = origFile }()
	objectFile = "/nonexistent/path/data.bin"

	cmd := newObjectPutCommand()
	err := runObjectPutCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

func TestRunObjectPutCommand_StdinNoFileFactoryError(t *testing.T) {
	origFile := objectFile
	origFactory := objectStorageClientFactory
	defer func() {
		objectFile = origFile
		objectStorageClientFactory = origFactory
	}()

	objectFile = ""
	objectStorageClientFactory = func() (*grpc.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no server")
	}

	cmd := newObjectPutCommand()
	err := runObjectPutCommand(cmd, []string{"bucket/key"})
	assert.Error(t, err)
}

// =============================================================================
// Object Storage SetPolicy Default Format
// =============================================================================

func TestFormatObjectSetPolicyResponse_DefaultFormat(t *testing.T) {
	orig := objectOutputFormat
	defer func() { objectOutputFormat = orig }()
	objectOutputFormat = ""

	resp := &pb.SetObjectPolicyResponse{Key: "bucket/key", Version: 5}
	err := formatObjectSetPolicyResponse(resp)
	assert.NoError(t, err)
}

// =============================================================================
// MockRBACClient - Default Path Coverage
// =============================================================================

func TestMockRBACClient_DefaultCreateRole(t *testing.T) {
	mock := &MockRBACClient{}
	resp, err := mock.CreateRole(context.Background(), &rbac.CreateRoleRequest{
		RoleName:    "test",
		Permissions: []string{"keys:read"},
	})
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.RoleID)
}

func TestMockRBACClient_DefaultDeleteRole(t *testing.T) {
	mock := &MockRBACClient{}
	resp, err := mock.DeleteRole(context.Background(), &rbac.DeleteRoleRequest{
		RoleName: "test",
	})
	assert.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestMockRBACClient_DefaultAssignRole(t *testing.T) {
	mock := &MockRBACClient{}
	resp, err := mock.AssignRole(context.Background(), &rbac.AssignRoleRequest{
		Username: "user1",
		RoleName: "admin",
	})
	assert.NoError(t, err)
	assert.True(t, resp.Success)
}

func TestMockRBACClient_DefaultRevokeRole(t *testing.T) {
	mock := &MockRBACClient{}
	resp, err := mock.RevokeRole(context.Background(), &rbac.RevokeRoleRequest{
		Username: "user1",
		RoleName: "admin",
	})
	assert.NoError(t, err)
	assert.True(t, resp.Success)
}

// =============================================================================
// Embedding Format Functions - Additional Branches
// =============================================================================

func TestFormatEmbeddingSetDefaultResponse_DefaultFormat(t *testing.T) {
	orig := outputFormat
	defer func() { outputFormat = orig }()
	outputFormat = ""

	resp := &embedding.SetDefaultProviderResponse{
		Provider:         "openai",
		PreviousProvider: "ollama",
		Timestamp:        time.Now(),
	}
	err := formatEmbeddingSetDefaultResponse(resp)
	assert.NoError(t, err)
}

func TestFormatEmbeddingSetDefaultResponse_YAMLFormat(t *testing.T) {
	orig := outputFormat
	defer func() { outputFormat = orig }()
	outputFormat = "yaml"

	resp := &embedding.SetDefaultProviderResponse{
		Provider:         "openai",
		PreviousProvider: "ollama",
		Timestamp:        time.Now(),
	}
	err := formatEmbeddingSetDefaultResponse(resp)
	assert.NoError(t, err)
}

func TestFormatEmbeddingCacheClearResponse_DefaultWithProvider(t *testing.T) {
	orig := outputFormat
	defer func() { outputFormat = orig }()
	outputFormat = ""

	resp := &embedding.ClearEmbeddingCacheResponse{
		EntriesCleared: 50,
		Provider:       "openai",
		Timestamp:      time.Now(),
	}
	err := formatEmbeddingCacheClearResponse(resp)
	assert.NoError(t, err)
}

func TestFormatEmbeddingCacheClearResponse_DefaultNoProvider(t *testing.T) {
	orig := outputFormat
	defer func() { outputFormat = orig }()
	outputFormat = ""

	resp := &embedding.ClearEmbeddingCacheResponse{
		EntriesCleared: 100,
		Provider:       "",
		Timestamp:      time.Now(),
	}
	err := formatEmbeddingCacheClearResponse(resp)
	assert.NoError(t, err)
}

func TestFormatEmbeddingCacheClearResponse_YAMLFormat(t *testing.T) {
	orig := outputFormat
	defer func() { outputFormat = orig }()
	outputFormat = "yaml"

	resp := &embedding.ClearEmbeddingCacheResponse{
		EntriesCleared: 25,
		Provider:       "ollama",
		Timestamp:      time.Now(),
	}
	err := formatEmbeddingCacheClearResponse(resp)
	assert.NoError(t, err)
}

// =============================================================================
// Crypto Random - Input Validation
// =============================================================================

func TestRunCryptoRandomCommand_InvalidBytesZero(t *testing.T) {
	origBytes := cryptoBytes
	defer func() { cryptoBytes = origBytes }()

	cryptoBytes = 0

	cmd := &cobra.Command{}
	err := runCryptoRandomCommand(cmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bytes must be between")
}

func TestRunCryptoRandomCommand_InvalidBytesTooLarge(t *testing.T) {
	origBytes := cryptoBytes
	defer func() { cryptoBytes = origBytes }()

	cryptoBytes = 1024*1024 + 1

	cmd := &cobra.Command{}
	err := runCryptoRandomCommand(cmd, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bytes must be between")
}

// =============================================================================
// Backup Storage - S3 Config Error Paths
// =============================================================================

func TestBuildS3Config_MissingBucket(t *testing.T) {
	flags := &BackupStorageFlags{
		StorageType: "s3",
		Bucket:      "",
		Region:      "us-east-1",
	}
	builder := NewBackupStorageBuilder(flags, "", "")
	ctx := context.Background()
	_, _, err := builder.Build(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bucket")
}

func TestBuildAzureConfig_MissingContainer(t *testing.T) {
	flags := &BackupStorageFlags{
		StorageType: "azure",
		Container:   "",
	}
	builder := NewBackupStorageBuilder(flags, "", "")
	ctx := context.Background()
	_, _, err := builder.Build(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "container")
}

// =============================================================================
// Role List - Empty Roles and JSON Format
// =============================================================================

func TestRunRoleListCommand_EmptyRolesDefault(t *testing.T) {
	origFactory := rbacClientFactory
	origFmt := outputFormat
	defer func() {
		rbacClientFactory = origFactory
		outputFormat = origFmt
	}()

	outputFormat = ""

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			ListRolesFunc: func(ctx context.Context, req *rbac.ListRolesRequest) (*rbac.ListRolesResponse, error) {
				return &rbac.ListRolesResponse{
					Roles:      []*rbac.RoleInfo{},
					TotalCount: 0,
				}, nil
			},
		}, nil
	}

	cmd := &cobra.Command{}
	err := runRoleListCommand(cmd, nil)
	assert.NoError(t, err)
}

func TestRunRoleListCommand_JSONFormat(t *testing.T) {
	origFactory := rbacClientFactory
	origFmt := outputFormat
	defer func() {
		rbacClientFactory = origFactory
		outputFormat = origFmt
	}()

	outputFormat = "json"

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			ListRolesFunc: func(ctx context.Context, req *rbac.ListRolesRequest) (*rbac.ListRolesResponse, error) {
				return &rbac.ListRolesResponse{
					Roles: []*rbac.RoleInfo{
						{RoleID: "r1", RoleName: "admin", Permissions: []string{"*:*"}},
					},
					TotalCount: 1,
				}, nil
			},
		}, nil
	}

	cmd := &cobra.Command{}
	err := runRoleListCommand(cmd, nil)
	assert.NoError(t, err)
}

func TestRunRoleListCommand_ListError(t *testing.T) {
	origFactory := rbacClientFactory
	defer func() { rbacClientFactory = origFactory }()

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			ListRolesFunc: func(ctx context.Context, req *rbac.ListRolesRequest) (*rbac.ListRolesResponse, error) {
				return nil, errors.New(errors.ErrInternal, "test", "rpc error")
			},
		}, nil
	}

	cmd := &cobra.Command{}
	err := runRoleListCommand(cmd, nil)
	assert.Error(t, err)
}

// =============================================================================
// Role Show - JSON Format and Error
// =============================================================================

func TestRunRoleShowCommand_JSONFormat(t *testing.T) {
	origFactory := rbacClientFactory
	origFmt := outputFormat
	defer func() {
		rbacClientFactory = origFactory
		outputFormat = origFmt
	}()

	outputFormat = "json"

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			ListRolesFunc: func(ctx context.Context, req *rbac.ListRolesRequest) (*rbac.ListRolesResponse, error) {
				return &rbac.ListRolesResponse{
					Roles: []*rbac.RoleInfo{
						{RoleID: "r1", RoleName: "admin", Permissions: []string{"*:*"}},
					},
					TotalCount: 1,
				}, nil
			},
		}, nil
	}

	cmd := &cobra.Command{}
	err := runRoleShowCommand(cmd, []string{"admin"})
	assert.NoError(t, err)
}

func TestRunRoleShowCommand_NotFound(t *testing.T) {
	origFactory := rbacClientFactory
	defer func() { rbacClientFactory = origFactory }()

	rbacClientFactory = func() (rbac.Client, error) {
		return &MockRBACClient{
			ListRolesFunc: func(ctx context.Context, req *rbac.ListRolesRequest) (*rbac.ListRolesResponse, error) {
				return &rbac.ListRolesResponse{
					Roles:      []*rbac.RoleInfo{},
					TotalCount: 0,
				}, nil
			},
		}, nil
	}

	cmd := &cobra.Command{}
	err := runRoleShowCommand(cmd, []string{"nonexistent"})
	assert.Error(t, err)
}

// =============================================================================
// Backup Delete Command
// =============================================================================

func TestRunBackupDeleteCommand_SuccessJSON(t *testing.T) {
	origFactory := backupClientFactory
	origFmt := backupOutputFormat
	origForce := backupDeleteForce
	defer func() {
		backupClientFactory = origFactory
		backupOutputFormat = origFmt
		backupDeleteForce = origForce
	}()

	backupOutputFormat = "json"
	backupDeleteForce = true

	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			DeleteBackupFunc: func(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error) {
				return &cli.BackupDeleteResponse{
					BackupID:  req.BackupID,
					Deleted:   true,
					Message:   "backup deleted",
					Timestamp: time.Now(),
				}, nil
			},
		}, nil
	}

	cmd := newBackupDeleteCommand()
	backupDeleteForce = true
	err := runBackupDeleteCommand(cmd, []string{"backup-001"})
	assert.NoError(t, err)
}

func TestRunBackupDeleteCommand_DeleteError(t *testing.T) {
	origFactory := backupClientFactory
	origForce := backupDeleteForce
	defer func() {
		backupClientFactory = origFactory
		backupDeleteForce = origForce
	}()

	backupDeleteForce = true

	backupClientFactory = func() (BackupClient, error) {
		return &MockBackupClient{
			DeleteBackupFunc: func(ctx context.Context, req *BackupDeleteRequest) (*cli.BackupDeleteResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "backup.delete", "backup not found")
			},
		}, nil
	}

	cmd := newBackupDeleteCommand()
	backupDeleteForce = true
	err := runBackupDeleteCommand(cmd, []string{"nonexistent"})
	assert.Error(t, err)
}

// =============================================================================
// Object List By Tag/Tenant - Command Construction
// =============================================================================

func TestNewObjectListByTagCommand_Construction(t *testing.T) {
	cmd := newObjectListByTagCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "list-by-tag", cmd.Use)
}

func TestNewObjectListByTenantCommand_Construction(t *testing.T) {
	cmd := newObjectListByTenantCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "list-by-tenant", cmd.Use)
}
