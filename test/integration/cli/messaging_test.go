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

package cli

import (
	"testing"
)

// =============================================================================
// Messaging Channel Lifecycle Tests
// =============================================================================

// TestMsgChannelLifecycle tests the complete messaging channel lifecycle across all transports.
func TestMsgChannelLifecycle(t *testing.T) {
	RunForAllTransports(t, "ChannelLifecycle", func(t *testing.T, runner *TestRunner) {
		channelName := UniqueKey("test-channel")
		suite := MsgChannelSuite(channelName)
		runner.RunSuite(suite)
	})
}

// TestMsgChannelCreate tests channel creation across all transports.
func TestMsgChannelCreate(t *testing.T) {
	RunForAllTransports(t, "ChannelCreate", func(t *testing.T, runner *TestRunner) {
		channelName := UniqueKey("create-channel")
		cmd := MsgChannelCreateCommand(channelName)
		runner.RunAndAssert(cmd)

		// Cleanup
		cleanup := MsgChannelDeleteCommand(channelName, true)
		runner.Run(cleanup)
	})
}

// TestMsgChannelCreateWithOptions tests channel creation with various options.
func TestMsgChannelCreateWithOptions(t *testing.T) {
	testCases := []struct {
		name           string
		bufferSize     int32
		maxSubscribers int32
		persistent     bool
		ttl            string
	}{
		{"CustomBuffer", 512, 0, false, ""},
		{"MaxSubscribers", 0, 5000, false, ""},
		{"Persistent", 0, 0, true, ""},
		{"WithTTL", 0, 0, false, "1h"},
		{"AllOptions", 1024, 10000, true, "30m"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			RunForAllTransports(t, "CreateWithOptions", func(t *testing.T, runner *TestRunner) {
				channelName := UniqueKey("options-channel")
				cmd := MsgChannelCreateWithOptionsCommand(channelName, tc.bufferSize, tc.maxSubscribers, tc.persistent, tc.ttl)
				runner.RunAndAssert(cmd)

				// Verify channel was created
				getCmd := MsgChannelGetCommand(channelName)
				runner.RunAndAssert(getCmd)

				// Cleanup
				cleanup := MsgChannelDeleteCommand(channelName, true)
				runner.Run(cleanup)
			})
		})
	}
}

// TestMsgChannelList tests listing channels across all transports.
func TestMsgChannelList(t *testing.T) {
	RunForAllTransports(t, "ChannelList", func(t *testing.T, runner *TestRunner) {
		// Create multiple channels
		channels := []string{
			UniqueKey("list-channel-1"),
			UniqueKey("list-channel-2"),
			UniqueKey("list-channel-3"),
		}

		for _, ch := range channels {
			createCmd := MsgChannelCreateCommand(ch)
			runner.RunAndAssert(createCmd)
		}

		// List channels
		listCmd := MsgChannelListCommand()
		runner.RunAndAssert(listCmd)

		// Cleanup
		for _, ch := range channels {
			cleanup := MsgChannelDeleteCommand(ch, true)
			runner.Run(cleanup)
		}
	})
}

// TestMsgChannelListWithFilter tests listing channels with filter across all transports.
func TestMsgChannelListWithFilter(t *testing.T) {
	RunForAllTransports(t, "ChannelListWithFilter", func(t *testing.T, runner *TestRunner) {
		prefix := UniqueKey("filter-test")
		channels := []string{
			prefix + "-alpha",
			prefix + "-beta",
			prefix + "-gamma",
		}

		// Create channels with common prefix
		for _, ch := range channels {
			createCmd := MsgChannelCreateCommand(ch)
			runner.RunAndAssert(createCmd)
		}

		// List with filter
		filterCmd := MsgChannelListWithFilterCommand(prefix+"*", 10, 0)
		runner.RunAndAssert(filterCmd)

		// List with pagination
		paginatedCmd := MsgChannelListWithFilterCommand("", 2, 0)
		runner.RunAndAssert(paginatedCmd)

		// Cleanup
		for _, ch := range channels {
			cleanup := MsgChannelDeleteCommand(ch, true)
			runner.Run(cleanup)
		}
	})
}

// TestMsgChannelGet tests getting channel information across all transports.
func TestMsgChannelGet(t *testing.T) {
	RunForAllTransports(t, "ChannelGet", func(t *testing.T, runner *TestRunner) {
		channelName := UniqueKey("get-channel")

		// Create channel
		createCmd := MsgChannelCreateCommand(channelName)
		runner.RunAndAssert(createCmd)

		// Get channel info
		getCmd := MsgChannelGetCommand(channelName)
		runner.RunAndAssert(getCmd)

		// Cleanup
		cleanup := MsgChannelDeleteCommand(channelName, true)
		runner.Run(cleanup)
	})
}

// TestMsgChannelDelete tests deleting channels across all transports.
func TestMsgChannelDelete(t *testing.T) {
	RunForAllTransports(t, "ChannelDelete", func(t *testing.T, runner *TestRunner) {
		channelName := UniqueKey("delete-channel")

		// Create channel
		createCmd := MsgChannelCreateCommand(channelName)
		runner.RunAndAssert(createCmd)

		// Delete channel with force flag
		deleteCmd := MsgChannelDeleteCommand(channelName, true)
		runner.RunAndAssert(deleteCmd)

		// Verify channel was deleted - get should fail
		getCmd := MsgChannelGetCommand(channelName)
		getCmd.ExpectSuccess = false
		stdout, stderr, err := runner.Run(getCmd)
		if err != nil {
			t.Logf("Get after delete correctly failed: stdout=%s stderr=%s", stdout, stderr)
		}
	})
}

// =============================================================================
// Messaging Publish Tests
// =============================================================================

// TestMsgPublish tests publishing messages to channels across all transports.
func TestMsgPublish(t *testing.T) {
	RunForAllTransports(t, "Publish", func(t *testing.T, runner *TestRunner) {
		channelName := UniqueKey("publish-channel")

		// Create channel
		createCmd := MsgChannelCreateCommand(channelName)
		runner.RunAndAssert(createCmd)

		// Publish a simple message
		payload := `{"event": "test", "data": "hello world"}`
		publishCmd := MsgPublishCommand(channelName, payload)
		runner.RunAndAssert(publishCmd)

		// Cleanup
		cleanup := MsgChannelDeleteCommand(channelName, true)
		runner.Run(cleanup)
	})
}

// TestMsgPublishWithOptions tests publishing messages with various options.
func TestMsgPublishWithOptions(t *testing.T) {
	testCases := []struct {
		name     string
		dedupID  string
		priority string
		ttl      string
	}{
		{"WithDedupID", "dedup-123", "", ""},
		{"WithPriority", "", "high", ""},
		{"WithTTL", "", "", "5m"},
		{"AllOptions", "dedup-456", "normal", "10m"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			RunForAllTransports(t, "PublishWithOptions", func(t *testing.T, runner *TestRunner) {
				channelName := UniqueKey("publish-options-channel")

				// Create channel
				createCmd := MsgChannelCreateCommand(channelName)
				runner.RunAndAssert(createCmd)

				// Publish with options
				payload := `{"test": "data"}`
				publishCmd := MsgPublishWithOptionsCommand(channelName, payload, tc.dedupID, tc.priority, tc.ttl)
				runner.RunAndAssert(publishCmd)

				// Cleanup
				cleanup := MsgChannelDeleteCommand(channelName, true)
				runner.Run(cleanup)
			})
		})
	}
}

// TestMsgPublishMultiple tests publishing multiple messages to verify channel functionality.
func TestMsgPublishMultiple(t *testing.T) {
	RunForAllTransports(t, "PublishMultiple", func(t *testing.T, runner *TestRunner) {
		channelName := UniqueKey("multi-publish-channel")

		// Create channel
		createCmd := MsgChannelCreateCommand(channelName)
		runner.RunAndAssert(createCmd)

		// Publish multiple messages
		for i := 0; i < 5; i++ {
			payload := `{"sequence": ` + string(rune('0'+i)) + `, "message": "test message"}`
			publishCmd := MsgPublishCommand(channelName, payload)
			runner.RunAndAssert(publishCmd)
		}

		// Cleanup
		cleanup := MsgChannelDeleteCommand(channelName, true)
		runner.Run(cleanup)
	})
}

// TestMsgPublishDelta tests publishing delta messages across all transports.
func TestMsgPublishDelta(t *testing.T) {
	RunForAllTransports(t, "PublishDelta", func(t *testing.T, runner *TestRunner) {
		channelName := UniqueKey("delta-channel")

		// Create channel
		createCmd := MsgChannelCreateCommand(channelName)
		runner.RunAndAssert(createCmd)

		// Publish delta message
		oldValue := `{"status": "pending", "count": 10}`
		newValue := `{"status": "completed", "count": 15}`
		deltaCmd := MsgPublishDeltaCommand(channelName, oldValue, newValue)
		runner.RunAndAssert(deltaCmd)

		// Cleanup
		cleanup := MsgChannelDeleteCommand(channelName, true)
		runner.Run(cleanup)
	})
}

// TestMsgPublishDeltaWithEntity tests publishing delta messages with entity information.
func TestMsgPublishDeltaWithEntity(t *testing.T) {
	RunForAllTransports(t, "PublishDeltaWithEntity", func(t *testing.T, runner *TestRunner) {
		channelName := UniqueKey("delta-entity-channel")

		// Create channel
		createCmd := MsgChannelCreateCommand(channelName)
		runner.RunAndAssert(createCmd)

		// Publish delta with entity info
		oldValue := `{"amount": 100.00}`
		newValue := `{"amount": 150.00}`
		deltaCmd := MsgPublishDeltaWithEntityCommand(channelName, oldValue, newValue, "order-12345", "Order")
		runner.RunAndAssert(deltaCmd)

		// Cleanup
		cleanup := MsgChannelDeleteCommand(channelName, true)
		runner.Run(cleanup)
	})
}

// =============================================================================
// Messaging Subscription Tests
// =============================================================================

// TestMsgListSubscriptions tests listing subscriptions across all transports.
func TestMsgListSubscriptions(t *testing.T) {
	RunForAllTransports(t, "ListSubscriptions", func(t *testing.T, runner *TestRunner) {
		// List all subscriptions
		listCmd := MsgListSubscriptionsCommand()
		runner.RunAndAssert(listCmd)
	})
}

// TestMsgListSubscriptionsByChannel tests listing subscriptions for a specific channel.
func TestMsgListSubscriptionsByChannel(t *testing.T) {
	RunForAllTransports(t, "ListSubscriptionsByChannel", func(t *testing.T, runner *TestRunner) {
		channelName := UniqueKey("subs-channel")

		// Create channel
		createCmd := MsgChannelCreateCommand(channelName)
		runner.RunAndAssert(createCmd)

		// List subscriptions for the channel
		listCmd := MsgListSubscriptionsByChannelCommand(channelName)
		runner.RunAndAssert(listCmd)

		// Cleanup
		cleanup := MsgChannelDeleteCommand(channelName, true)
		runner.Run(cleanup)
	})
}

// =============================================================================
// Messaging Statistics Tests
// =============================================================================

// TestMsgDedupStats tests getting deduplication statistics across all transports.
func TestMsgDedupStats(t *testing.T) {
	RunForAllTransports(t, "DedupStats", func(t *testing.T, runner *TestRunner) {
		channelName := UniqueKey("stats-channel")

		// Create channel
		createCmd := MsgChannelCreateCommand(channelName)
		runner.RunAndAssert(createCmd)

		// Publish some messages to generate stats
		for i := 0; i < 3; i++ {
			payload := `{"message": "test"}`
			publishCmd := MsgPublishCommand(channelName, payload)
			runner.RunAndAssert(publishCmd)
		}

		// Get dedup stats
		statsCmd := MsgDedupStatsCommand(channelName)
		runner.RunAndAssert(statsCmd)

		// Cleanup
		cleanup := MsgChannelDeleteCommand(channelName, true)
		runner.Run(cleanup)
	})
}

// TestMsgDedupStatsWithOptions tests getting dedup stats with options.
func TestMsgDedupStatsWithOptions(t *testing.T) {
	testCases := []struct {
		name   string
		reset  bool
		window string
	}{
		{"WithWindow", false, "1h"},
		{"WithReset", true, ""},
		{"WithBothOptions", true, "30m"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			RunForAllTransports(t, "DedupStatsWithOptions", func(t *testing.T, runner *TestRunner) {
				channelName := UniqueKey("stats-options-channel")

				// Create channel
				createCmd := MsgChannelCreateCommand(channelName)
				runner.RunAndAssert(createCmd)

				// Get dedup stats with options
				statsCmd := MsgDedupStatsWithOptionsCommand(channelName, tc.reset, tc.window)
				runner.RunAndAssert(statsCmd)

				// Cleanup
				cleanup := MsgChannelDeleteCommand(channelName, true)
				runner.Run(cleanup)
			})
		})
	}
}

// =============================================================================
// Messaging Help Command Tests
// =============================================================================

// TestMsgHelp tests the messaging help command.
func TestMsgHelp(t *testing.T) {
	RunForAllTransports(t, "Help", func(t *testing.T, runner *TestRunner) {
		cmd := MsgHelpCommand()
		runner.RunAndAssert(cmd)
	})
}

// TestMsgHelpSuite tests all messaging help commands.
func TestMsgHelpSuite(t *testing.T) {
	RunForAllTransports(t, "HelpSuite", func(t *testing.T, runner *TestRunner) {
		suite := MsgHelpSuite()
		runner.RunSuite(suite)
	})
}

// =============================================================================
// Messaging Error Handling Tests
// =============================================================================

// TestMsgChannelCreateMissingName tests error handling for missing channel name.
func TestMsgChannelCreateMissingName(t *testing.T) {
	RunForAllTransports(t, "CreateMissingName", func(t *testing.T, runner *TestRunner) {
		cmd := MsgChannelCreateMissingNameCommand()
		runner.RunAndAssert(cmd)
	})
}

// TestMsgPublishMissingPayload tests error handling for missing payload.
func TestMsgPublishMissingPayload(t *testing.T) {
	RunForAllTransports(t, "PublishMissingPayload", func(t *testing.T, runner *TestRunner) {
		channelName := UniqueKey("error-channel")

		// Create channel first
		createCmd := MsgChannelCreateCommand(channelName)
		runner.RunAndAssert(createCmd)

		// Try to publish without payload
		cmd := MsgPublishMissingPayloadCommand(channelName)
		runner.RunAndAssert(cmd)

		// Cleanup
		cleanup := MsgChannelDeleteCommand(channelName, true)
		runner.Run(cleanup)
	})
}

// TestMsgPublishDeltaMissingValues tests error handling for missing delta values.
func TestMsgPublishDeltaMissingValues(t *testing.T) {
	RunForAllTransports(t, "PublishDeltaMissingValues", func(t *testing.T, runner *TestRunner) {
		channelName := UniqueKey("delta-error-channel")

		// Create channel first
		createCmd := MsgChannelCreateCommand(channelName)
		runner.RunAndAssert(createCmd)

		// Try to publish delta without values
		cmd := MsgPublishDeltaMissingValuesCommand(channelName)
		runner.RunAndAssert(cmd)

		// Cleanup
		cleanup := MsgChannelDeleteCommand(channelName, true)
		runner.Run(cleanup)
	})
}

// TestMsgErrorSuite tests all messaging error commands.
func TestMsgErrorSuite(t *testing.T) {
	RunForAllTransports(t, "ErrorSuite", func(t *testing.T, runner *TestRunner) {
		channelName := UniqueKey("error-suite-channel")

		// Create channel for error testing
		createCmd := MsgChannelCreateCommand(channelName)
		runner.RunAndAssert(createCmd)

		suite := MsgErrorSuite(channelName)
		runner.RunSuite(suite)

		// Cleanup
		cleanup := MsgChannelDeleteCommand(channelName, true)
		runner.Run(cleanup)
	})
}

// =============================================================================
// Messaging Full Workflow Tests
// =============================================================================

// TestMsgFullChannelWorkflow tests a complete messaging workflow.
func TestMsgFullChannelWorkflow(t *testing.T) {
	RunForAllTransports(t, "FullChannelWorkflow", func(t *testing.T, runner *TestRunner) {
		channelName := UniqueKey("workflow-channel")

		// 1. Create channel with options
		createCmd := MsgChannelCreateWithOptionsCommand(channelName, 512, 1000, false, "1h")
		runner.RunAndAssert(createCmd)

		// 2. Get channel info
		getCmd := MsgChannelGetCommand(channelName)
		runner.RunAndAssert(getCmd)

		// 3. List channels
		listCmd := MsgChannelListCommand()
		runner.RunAndAssert(listCmd)

		// 4. Publish simple message
		payload := `{"event": "workflow_test", "timestamp": "2025-01-01T00:00:00Z"}`
		publishCmd := MsgPublishCommand(channelName, payload)
		runner.RunAndAssert(publishCmd)

		// 5. Publish message with options
		publishOptCmd := MsgPublishWithOptionsCommand(channelName, payload, "dedup-workflow-1", "high", "30m")
		runner.RunAndAssert(publishOptCmd)

		// 6. Publish delta message
		oldVal := `{"status": "pending"}`
		newVal := `{"status": "active"}`
		deltaCmd := MsgPublishDeltaCommand(channelName, oldVal, newVal)
		runner.RunAndAssert(deltaCmd)

		// 7. Publish delta with entity
		deltaEntityCmd := MsgPublishDeltaWithEntityCommand(channelName, oldVal, newVal, "entity-123", "TestEntity")
		runner.RunAndAssert(deltaEntityCmd)

		// 8. Get dedup stats
		dedupCmd := MsgDedupStatsCommand(channelName)
		runner.RunAndAssert(dedupCmd)

		// 9. List subscriptions
		subsCmd := MsgListSubscriptionsByChannelCommand(channelName)
		runner.RunAndAssert(subsCmd)

		// 10. Delete channel
		deleteCmd := MsgChannelDeleteCommand(channelName, true)
		runner.RunAndAssert(deleteCmd)
	})
}

// TestMsgMultipleChannels tests operations with multiple channels.
func TestMsgMultipleChannels(t *testing.T) {
	RunForAllTransports(t, "MultipleChannels", func(t *testing.T, runner *TestRunner) {
		basePrefix := UniqueKey("multi")
		channels := []string{
			basePrefix + "-orders",
			basePrefix + "-events",
			basePrefix + "-notifications",
		}

		// Create multiple channels
		for _, ch := range channels {
			createCmd := MsgChannelCreateCommand(ch)
			runner.RunAndAssert(createCmd)
		}

		// Publish to each channel
		for i, ch := range channels {
			payload := `{"channel_index": ` + string(rune('0'+i)) + `, "data": "test"}`
			publishCmd := MsgPublishCommand(ch, payload)
			runner.RunAndAssert(publishCmd)
		}

		// List all channels
		listCmd := MsgChannelListCommand()
		runner.RunAndAssert(listCmd)

		// List with prefix filter
		filterCmd := MsgChannelListWithFilterCommand(basePrefix+"*", 10, 0)
		runner.RunAndAssert(filterCmd)

		// Get info for each channel
		for _, ch := range channels {
			getCmd := MsgChannelGetCommand(ch)
			runner.RunAndAssert(getCmd)
		}

		// Cleanup
		for _, ch := range channels {
			cleanup := MsgChannelDeleteCommand(ch, true)
			runner.Run(cleanup)
		}
	})
}

// TestMsgDeduplication tests message deduplication functionality.
func TestMsgDeduplication(t *testing.T) {
	RunForAllTransports(t, "Deduplication", func(t *testing.T, runner *TestRunner) {
		channelName := UniqueKey("dedup-test-channel")

		// Create channel
		createCmd := MsgChannelCreateCommand(channelName)
		runner.RunAndAssert(createCmd)

		// Publish messages with same dedup ID
		dedupID := "unique-dedup-id-12345"
		payload := `{"event": "duplicate_test"}`

		// First publish should succeed
		publishCmd1 := MsgPublishWithOptionsCommand(channelName, payload, dedupID, "", "")
		runner.RunAndAssert(publishCmd1)

		// Second publish with same dedup ID
		publishCmd2 := MsgPublishWithOptionsCommand(channelName, payload, dedupID, "", "")
		runner.RunAndAssert(publishCmd2)

		// Third publish with same dedup ID
		publishCmd3 := MsgPublishWithOptionsCommand(channelName, payload, dedupID, "", "")
		runner.RunAndAssert(publishCmd3)

		// Check dedup stats - should show duplicates
		statsCmd := MsgDedupStatsCommand(channelName)
		runner.RunAndAssert(statsCmd)

		// Cleanup
		cleanup := MsgChannelDeleteCommand(channelName, true)
		runner.Run(cleanup)
	})
}

// TestMsgPersistentChannel tests persistent channel functionality.
func TestMsgPersistentChannel(t *testing.T) {
	RunForAllTransports(t, "PersistentChannel", func(t *testing.T, runner *TestRunner) {
		channelName := UniqueKey("persistent-channel")

		// Create persistent channel
		createCmd := MsgChannelCreateWithOptionsCommand(channelName, 256, 1000, true, "")
		runner.RunAndAssert(createCmd)

		// Publish messages to persistent channel
		for i := 0; i < 5; i++ {
			payload := `{"seq": ` + string(rune('0'+i)) + `, "persistent": true}`
			publishCmd := MsgPublishCommand(channelName, payload)
			runner.RunAndAssert(publishCmd)
		}

		// Get channel info to verify persistence
		getCmd := MsgChannelGetCommand(channelName)
		stdout, _, err := runner.Run(getCmd)
		if err == nil {
			t.Logf("Persistent channel info: %s", stdout)
		}

		// Cleanup
		cleanup := MsgChannelDeleteCommand(channelName, true)
		runner.Run(cleanup)
	})
}
