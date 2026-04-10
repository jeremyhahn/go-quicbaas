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
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelCommand_Structure(t *testing.T) {
	t.Run("HasServeSubcommand", func(t *testing.T) {
		cmd := newChannelCommand()
		require.NotNil(t, cmd)

		assert.Equal(t, "channel", cmd.Use)
		assert.Contains(t, cmd.Short, "Channel server")

		// Verify serve subcommand exists.
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Use == "serve" {
				found = true
				break
			}
		}
		assert.True(t, found, "expected 'serve' subcommand")
	})

	t.Run("ChannelCommandRequiresSubcommand", func(t *testing.T) {
		cmd := newChannelCommand()
		cmd.SetArgs([]string{})
		err := cmd.Execute()
		assert.Error(t, err, "expected error when no subcommand is provided")
	})
}

func TestChannelCommand_Help(t *testing.T) {
	t.Run("ChannelHelp", func(t *testing.T) {
		cmd := newChannelCommand()
		cmd.SetArgs([]string{"--help"})
		err := cmd.Execute()
		assert.NoError(t, err)
	})

	t.Run("ServeHelp", func(t *testing.T) {
		cmd := newChannelCommand()
		cmd.SetArgs([]string{"serve", "--help"})
		err := cmd.Execute()
		assert.NoError(t, err)
	})
}

func TestChannelServeCommand_Flags(t *testing.T) {
	t.Run("AllFlagsRegistered", func(t *testing.T) {
		cmd := newChannelServeCommand()
		require.NotNil(t, cmd)

		expectedFlags := []string{
			"name",
			"address",
			"enable-reply",
			"enable-permission-relay",
			"instructions",
		}
		for _, flag := range expectedFlags {
			f := cmd.Flags().Lookup(flag)
			assert.NotNil(t, f, "expected flag %q to be registered", flag)
		}
	})

	t.Run("DefaultValues", func(t *testing.T) {
		cmd := newChannelServeCommand()

		nameFlag := cmd.Flags().Lookup("name")
		require.NotNil(t, nameFlag)
		assert.Equal(t, "quicbaas", nameFlag.DefValue)

		addressFlag := cmd.Flags().Lookup("address")
		require.NotNil(t, addressFlag)
		assert.Equal(t, "", addressFlag.DefValue)

		replyFlag := cmd.Flags().Lookup("enable-reply")
		require.NotNil(t, replyFlag)
		assert.Equal(t, "false", replyFlag.DefValue)

		permFlag := cmd.Flags().Lookup("enable-permission-relay")
		require.NotNil(t, permFlag)
		assert.Equal(t, "false", permFlag.DefValue)

		instrFlag := cmd.Flags().Lookup("instructions")
		require.NotNil(t, instrFlag)
		assert.Equal(t, "", instrFlag.DefValue)
	})

	t.Run("FlagUsageDescriptions", func(t *testing.T) {
		cmd := newChannelServeCommand()

		nameFlag := cmd.Flags().Lookup("name")
		require.NotNil(t, nameFlag)
		assert.NotEmpty(t, nameFlag.Usage)

		addressFlag := cmd.Flags().Lookup("address")
		require.NotNil(t, addressFlag)
		assert.NotEmpty(t, addressFlag.Usage)
	})
}

func TestChannelServeCommand_AddressRequired(t *testing.T) {
	t.Run("MissingAddress", func(t *testing.T) {
		// Reset the global flag to empty before this test.
		oldAddr := channelAddress
		channelAddress = ""
		defer func() { channelAddress = oldAddr }()

		cmd := newChannelServeCommand()
		cmd.SetArgs([]string{})
		err := cmd.RunE(cmd, []string{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "address is required")
	})
}

func TestHandleChannelMessage(t *testing.T) {
	t.Run("Initialize", func(t *testing.T) {
		// Set channel flags for the test.
		oldName := channelName
		oldReply := channelEnableReply
		oldPerm := channelEnablePermRelay
		oldInstr := channelInstructions
		channelName = "test-channel"
		channelEnableReply = true
		channelEnablePermRelay = false
		channelInstructions = "test instructions"
		defer func() {
			channelName = oldName
			channelEnableReply = oldReply
			channelEnablePermRelay = oldPerm
			channelInstructions = oldInstr
		}()

		msg := &channelMessage{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "initialize",
		}

		resp := handleChannelMessage(context.Background(), msg)
		require.NotNil(t, resp)
		assert.Equal(t, "2.0", resp.JSONRPC)
		assert.Nil(t, resp.Error)
		assert.NotNil(t, resp.Result)

		// Verify response structure by marshaling and unmarshaling.
		resultBytes, err := json.Marshal(resp.Result)
		require.NoError(t, err)

		var result map[string]interface{}
		err = json.Unmarshal(resultBytes, &result)
		require.NoError(t, err)

		serverInfo, ok := result["server_info"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "test-channel", serverInfo["name"])

		caps, ok := result["capabilities"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, caps["reply"])
		assert.Equal(t, false, caps["permission_relay"])

		assert.Equal(t, "test instructions", result["instructions"])
	})

	t.Run("InitializeWithoutInstructions", func(t *testing.T) {
		oldInstr := channelInstructions
		channelInstructions = ""
		defer func() { channelInstructions = oldInstr }()

		msg := &channelMessage{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`2`),
			Method:  "initialize",
		}

		resp := handleChannelMessage(context.Background(), msg)
		require.NotNil(t, resp)

		resultBytes, err := json.Marshal(resp.Result)
		require.NoError(t, err)

		var result map[string]interface{}
		err = json.Unmarshal(resultBytes, &result)
		require.NoError(t, err)

		_, hasInstructions := result["instructions"]
		assert.False(t, hasInstructions, "instructions should be omitted when empty")
	})

	t.Run("Ping", func(t *testing.T) {
		msg := &channelMessage{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`3`),
			Method:  "ping",
		}

		resp := handleChannelMessage(context.Background(), msg)
		require.NotNil(t, resp)
		assert.Equal(t, "2.0", resp.JSONRPC)
		assert.Nil(t, resp.Error)

		resultBytes, err := json.Marshal(resp.Result)
		require.NoError(t, err)

		var result map[string]string
		err = json.Unmarshal(resultBytes, &result)
		require.NoError(t, err)
		assert.Equal(t, "ok", result["status"])
	})

	t.Run("UnknownMethod", func(t *testing.T) {
		msg := &channelMessage{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`4`),
			Method:  "nonexistent",
		}

		resp := handleChannelMessage(context.Background(), msg)
		require.NotNil(t, resp)
		assert.NotNil(t, resp.Error)
		assert.Equal(t, -32601, resp.Error.Code)
		assert.Contains(t, resp.Error.Message, "method not found")
		assert.Contains(t, resp.Error.Message, "nonexistent")
	})

	t.Run("EmptyMethod", func(t *testing.T) {
		msg := &channelMessage{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`5`),
			Method:  "",
		}

		resp := handleChannelMessage(context.Background(), msg)
		require.NotNil(t, resp)
		assert.NotNil(t, resp.Error)
		assert.Equal(t, -32601, resp.Error.Code)
	})
}

func TestChannelAddressRequiredError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &ChannelAddressRequiredError{}
		assert.Contains(t, err.Error(), "address is required")
	})
}

func TestChannelDecodeError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		cause := &json.SyntaxError{}
		err := &ChannelDecodeError{Cause: cause}
		assert.Contains(t, err.Error(), "failed to decode JSON-RPC message")
	})

	t.Run("Unwrap", func(t *testing.T) {
		cause := &json.SyntaxError{}
		err := &ChannelDecodeError{Cause: cause}
		assert.Equal(t, cause, err.Unwrap())
	})
}

func TestChannelEncodeError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		cause := &json.SyntaxError{}
		err := &ChannelEncodeError{Cause: cause}
		assert.Contains(t, err.Error(), "failed to encode JSON-RPC response")
	})

	t.Run("Unwrap", func(t *testing.T) {
		cause := &json.SyntaxError{}
		err := &ChannelEncodeError{Cause: cause}
		assert.Equal(t, cause, err.Unwrap())
	})
}

func TestChannelResponse_JSONSerialization(t *testing.T) {
	t.Run("SuccessResponse", func(t *testing.T) {
		resp := &channelResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Result:  map[string]string{"status": "ok"},
		}

		data, err := json.Marshal(resp)
		require.NoError(t, err)

		var decoded map[string]interface{}
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		assert.Equal(t, "2.0", decoded["jsonrpc"])
	})

	t.Run("ErrorResponse", func(t *testing.T) {
		resp := &channelResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Error: &channelRPCError{
				Code:    -32601,
				Message: "method not found",
			},
		}

		data, err := json.Marshal(resp)
		require.NoError(t, err)

		var decoded map[string]interface{}
		err = json.Unmarshal(data, &decoded)
		require.NoError(t, err)

		errObj, ok := decoded["error"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, float64(-32601), errObj["code"])
		assert.Equal(t, "method not found", errObj["message"])
	})
}
