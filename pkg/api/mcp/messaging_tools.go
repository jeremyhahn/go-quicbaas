//go:build mcp

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

// Package mcp implements Messaging tool definitions for the Model Context Protocol.
package mcp

// Messaging tool definitions for pub/sub messaging operations (10 tools).
// Each tool follows the MCP specification with JSON schema validation.

var (
	// MsgCreateChannelTool creates a new messaging channel.
	MsgCreateChannelTool = Tool{
		Name:        "quicbaas_msg_create_channel",
		Description: "Create a new messaging channel with optional configuration",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"name": {
					Type:        "string",
					Description: "Unique channel name (alphanumeric, underscore, hyphen, period, slash, colon allowed)",
				},
				"codec": {
					Type:        "string",
					Description: "Default message codec for the channel",
					Enum:        []string{"json", "msgpack", "protobuf"},
				},
				"buffer_size": {
					Type:        "string",
					Description: "Message buffer size for the channel",
				},
				"max_subscribers": {
					Type:        "string",
					Description: "Maximum number of subscribers allowed",
				},
				"persistent": {
					Type:        "string",
					Description: "Set to 'true' to persist messages for replay",
				},
				"max_history": {
					Type:        "string",
					Description: "Maximum number of messages to retain for replay",
				},
				"ttl": {
					Type:        "string",
					Description: "Message time-to-live as duration string (e.g., '1h', '24h')",
				},
			},
			Required: []string{"name"},
		},
	}

	// MsgDeleteChannelTool deletes a messaging channel.
	MsgDeleteChannelTool = Tool{
		Name:        "quicbaas_msg_delete_channel",
		Description: "Delete a messaging channel and all its subscriptions",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"name": {
					Type:        "string",
					Description: "Name of the channel to delete",
				},
			},
			Required: []string{"name"},
		},
	}

	// MsgGetChannelTool retrieves information about a messaging channel.
	MsgGetChannelTool = Tool{
		Name:        "quicbaas_msg_get_channel",
		Description: "Get information about a messaging channel",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"name": {
					Type:        "string",
					Description: "Name of the channel to retrieve",
				},
			},
			Required: []string{"name"},
		},
	}

	// MsgListChannelsTool lists all messaging channels.
	MsgListChannelsTool = Tool{
		Name:        "quicbaas_msg_list_channels",
		Description: "List all messaging channels",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{},
			Required:   []string{},
		},
	}

	// MsgSubscribeTool subscribes to a messaging channel.
	MsgSubscribeTool = Tool{
		Name:        "quicbaas_msg_subscribe",
		Description: "Subscribe to a messaging channel",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"channel": {
					Type:        "string",
					Description: "Name of the channel to subscribe to",
				},
				"buffer_size": {
					Type:        "string",
					Description: "Subscription message buffer size",
				},
				"session_id": {
					Type:        "string",
					Description: "Optional session ID for the subscription",
				},
				"filter_key_prefix": {
					Type:        "string",
					Description: "Filter messages by key prefix",
				},
				"filter_event_types": {
					Type:        "string",
					Description: "JSON array of event types to filter (PUT, DELETE)",
				},
			},
			Required: []string{"channel"},
		},
	}

	// MsgWatchChannelTool watches the keys under a prefix: the MCP face of
	// Messaging.WatchChannel.
	MsgWatchChannelTool = Tool{
		Name:        "quicbaas_msg_watch_channel",
		Description: "Watch the keys under a key prefix for changes",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"key_prefix": {
					Type:        "string",
					Description: "Key prefix to watch",
				},
				"buffer_size": {
					Type:        "string",
					Description: "Subscription message buffer size",
				},
				"session_id": {
					Type:        "string",
					Description: "Optional session ID for the watch",
				},
				"filter_key_prefix": {
					Type:        "string",
					Description: "Filter events by a narrower key prefix",
				},
				"filter_event_types": {
					Type:        "string",
					Description: "JSON array of event types to filter (PUT, DELETE)",
				},
			},
			Required: []string{"key_prefix"},
		},
	}

	// MsgUnsubscribeTool removes a subscription from a channel.
	MsgUnsubscribeTool = Tool{
		Name:        "quicbaas_msg_unsubscribe",
		Description: "Unsubscribe from a messaging channel",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"subscription_id": {
					Type:        "string",
					Description: "ID of the subscription to remove",
				},
			},
			Required: []string{"subscription_id"},
		},
	}

	// MsgPublishTool publishes a message to a channel.
	MsgPublishTool = Tool{
		Name:        "quicbaas_msg_publish",
		Description: "Publish a message to a messaging channel",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"channel": {
					Type:        "string",
					Description: "Name of the channel to publish to",
				},
				"payload": {
					Type:        "string",
					Description: "Message payload (JSON string or base64-encoded binary)",
				},
				"message_id": {
					Type:        "string",
					Description: "Optional unique message ID for deduplication",
				},
				"dedup_key": {
					Type:        "string",
					Description: "Optional deduplication key",
				},
				"metadata": {
					Type:        "string",
					Description: "JSON object containing message metadata",
				},
			},
			Required: []string{"channel", "payload"},
		},
	}

	// MsgPublishDeltaTool publishes a delta update to a channel.
	MsgPublishDeltaTool = Tool{
		Name:        "quicbaas_msg_publish_delta",
		Description: "Publish a delta update (diff between old and new state) to a channel",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"channel": {
					Type:        "string",
					Description: "Name of the channel to publish to",
				},
				"old_state": {
					Type:        "string",
					Description: "JSON representation of the old state",
				},
				"new_state": {
					Type:        "string",
					Description: "JSON representation of the new state",
				},
				"message_id": {
					Type:        "string",
					Description: "Optional unique message ID for deduplication",
				},
				"dedup_key": {
					Type:        "string",
					Description: "Optional deduplication key",
				},
			},
			Required: []string{"channel", "old_state", "new_state"},
		},
	}

	// MsgStatsTool retrieves messaging service statistics.
	MsgStatsTool = Tool{
		Name:        "quicbaas_msg_stats",
		Description: "Get messaging service statistics including channels, subscriptions, and message counts",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{},
			Required:   []string{},
		},
	}

	// MsgChannelStatsTool returns statistics for a specific channel.
	MsgChannelStatsTool = Tool{
		Name:        "quicbaas_msg_channel_stats",
		Description: "Get statistics for a specific channel",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"channel": {
					Type:        "string",
					Description: "Name of the channel to get stats for",
				},
			},
			Required: []string{"channel"},
		},
	}

	// MsgDedupStatsTool retrieves deduplication statistics for a channel.
	MsgDedupStatsTool = Tool{
		Name:        "quicbaas_msg_dedup_stats",
		Description: "Get deduplication statistics for a specific channel",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"channel": {
					Type:        "string",
					Description: "Name of the channel to get dedup stats for",
				},
			},
			Required: []string{"channel"},
		},
	}
)

// getAllMsgTools returns all Messaging tools (10 tools).
func getAllMsgTools() []Tool {
	return []Tool{
		MsgCreateChannelTool,
		MsgDeleteChannelTool,
		MsgGetChannelTool,
		MsgListChannelsTool,
		MsgSubscribeTool,
		MsgWatchChannelTool,
		MsgUnsubscribeTool,
		MsgPublishTool,
		MsgPublishDeltaTool,
		MsgStatsTool,
		MsgChannelStatsTool,
		MsgDedupStatsTool,
	}
}
