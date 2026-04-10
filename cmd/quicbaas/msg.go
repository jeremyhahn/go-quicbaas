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

// Package main provides messaging CLI commands for QuicBaaS.
package main

import (
	stdcontext "context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Messaging command flags.
var (
	// Channel flags.
	msgBufferSize     int32
	msgMaxSubscribers int32
	msgPersistent     bool
	msgTTL            string
	msgDescription    string
	msgForce          bool

	// List/pagination flags.
	msgLimit  int32
	msgOffset int32
	msgFilter string

	// Subscribe flags.
	msgDelta     bool
	msgDedup     bool
	msgDedupSkip bool
	msgReplay    uint64

	// Publish flags.
	msgPayload  string
	msgFile     string
	msgDedupID  string
	msgPriority string
	msgAck      bool

	// Publish delta flags.
	msgOld        string
	msgNew        string
	msgOldFile    string
	msgNewFile    string
	msgEntityID   string
	msgEntityType string

	// Watch flags.
	msgTimestamps bool
	msgRaw        bool

	// Dedup stats flags.
	msgReset  bool
	msgWindow string

	// Subscription list flags.
	msgChannel string

	// Output format.
	msgOutputFmt string
)

// MessagingClient defines the interface for messaging operations.
type MessagingClient interface {
	MessagingCreateChannel(ctx stdcontext.Context, req *grpc.MessagingCreateChannelRequest) (*grpc.MessagingCreateChannelResponse, error)
	MessagingDeleteChannel(ctx stdcontext.Context, req *grpc.MessagingDeleteChannelRequest) (*grpc.MessagingDeleteChannelResponse, error)
	MessagingGetChannel(ctx stdcontext.Context, req *grpc.MessagingGetChannelRequest) (*grpc.MessagingGetChannelResponse, error)
	MessagingListChannels(ctx stdcontext.Context, req *grpc.MessagingListChannelsRequest) (*grpc.MessagingListChannelsResponse, error)
	MessagingSubscribe(ctx stdcontext.Context, req *grpc.MessagingSubscribeRequest) (*grpc.MessagingSubscribeResponse, error)
	MessagingUnsubscribe(ctx stdcontext.Context, req *grpc.MessagingUnsubscribeRequest) (*grpc.MessagingUnsubscribeResponse, error)
	MessagingPublish(ctx stdcontext.Context, req *grpc.MessagingPublishRequest) (*grpc.MessagingPublishResponse, error)
	MessagingPublishDelta(ctx stdcontext.Context, req *grpc.MessagingPublishDeltaRequest) (*grpc.MessagingPublishDeltaResponse, error)
	MessagingStreamNotifications(ctx stdcontext.Context, req *grpc.MessagingStreamNotificationsRequest) (<-chan *grpc.MessagingNotification, <-chan error, error)
	MessagingGetDedupStats(ctx stdcontext.Context, req *grpc.MessagingGetDedupStatsRequest) (*grpc.MessagingGetDedupStatsResponse, error)
	MessagingGetChannelStats(ctx stdcontext.Context, req *grpc.MessagingGetChannelStatsRequest) (*grpc.MessagingGetChannelStatsResponse, error)
	Close() error
}

// MessagingClientFactory is a function type for creating messaging clients.
type MessagingClientFactory func() (MessagingClient, error)

// messagingClientFactory creates messaging clients. Defaults to createMessagingClient.
var messagingClientFactory MessagingClientFactory = createMessagingClient

// createMessagingClient creates a client for messaging operations based on transport.
func createMessagingClient() (MessagingClient, error) {
	switch transport {
	case "grpc", "":
		return createMessagingGRPCClient()
	case "rest":
		return nil, errors.Newf(
			errors.ErrConfiguration,
			"createMessagingClient",
			"REST transport is not yet implemented for messaging commands",
		)
	case "mcp":
		return nil, errors.Newf(
			errors.ErrConfiguration,
			"createMessagingClient",
			"MCP transport is not yet implemented for messaging commands",
		)
	case "quic":
		return nil, errors.Newf(
			errors.ErrConfiguration,
			"createMessagingClient",
			"QUIC transport is not yet implemented for messaging commands",
		)
	default:
		return nil, errors.Newf(
			errors.ErrConfiguration,
			"createMessagingClient",
			"unsupported transport: %s (supported: grpc, rest, mcp, quic)", transport,
		)
	}
}

// createMessagingGRPCClient creates a gRPC client for messaging operations.
// TLS configuration is always included since gRPC requires TLS for all connections.
func createMessagingGRPCClient() (MessagingClient, error) {
	opts := []grpc.ClientOption{
		grpc.WithTimeout(timeout),
	}
	opts = append(opts, grpcTLSOption()...)
	return grpc.NewClient(address, opts...)
}

// newMsgCommands creates the messaging command group.
func newMsgCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "msg",
		Short: "Messaging commands (channels, subscriptions, publishing)",
		Long: `Messaging commands for managing notification channels and subscriptions.

These commands allow you to create channels, subscribe to notifications,
publish messages, and monitor real-time events in QuicBaaS.`,
	}

	cmd.PersistentFlags().StringVar(&msgOutputFmt, "output", "table", "Output format (json, yaml, table)")

	cmd.AddCommand(newMsgCreateChannelCommand())
	cmd.AddCommand(newMsgDeleteChannelCommand())
	cmd.AddCommand(newMsgListChannelsCommand())
	cmd.AddCommand(newMsgChannelInfoCommand())
	cmd.AddCommand(newMsgSubscribeCommand())
	cmd.AddCommand(newMsgUnsubscribeCommand())
	cmd.AddCommand(newMsgListSubscriptionsCommand())
	cmd.AddCommand(newMsgPublishCommand())
	cmd.AddCommand(newMsgPublishDeltaCommand())
	cmd.AddCommand(newMsgWatchCommand())
	cmd.AddCommand(newMsgDedupStatsCommand())

	return cmd
}

// =============================================================================
// Create Channel Command
// =============================================================================

// newMsgCreateChannelCommand creates the create-channel command.
func newMsgCreateChannelCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-channel <name>",
		Short: "Create a notification channel",
		Long: `Create a notification channel for publishing messages.

Examples:
  quicbaas msg create-channel orders
  quicbaas msg create-channel events --buffer-size 512 --ttl 1h
  quicbaas msg create-channel notifications --persistent --max-subscribers 5000`,
		Args: cobra.ExactArgs(1),
		RunE: runMsgCreateChannelCommand,
	}

	cmd.Flags().Int32Var(&msgBufferSize, "buffer-size", 256, "Channel buffer size")
	cmd.Flags().Int32Var(&msgMaxSubscribers, "max-subscribers", 1000, "Maximum subscribers allowed")
	cmd.Flags().BoolVar(&msgPersistent, "persistent", false, "Enable message persistence")
	cmd.Flags().StringVar(&msgTTL, "ttl", "0s", "Message time-to-live")
	cmd.Flags().StringVar(&msgDescription, "description", "", "Channel description")

	return cmd
}

// runMsgCreateChannelCommand executes the create-channel command.
func runMsgCreateChannelCommand(cmd *cobra.Command, args []string) error {
	channelName := args[0]

	client, err := messagingClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	// Parse TTL
	var ttlSeconds int64
	if msgTTL != "" && msgTTL != "0s" {
		ttlDuration, parseErr := time.ParseDuration(msgTTL)
		if parseErr != nil {
			return errors.Wrapf(parseErr, errors.ErrInvalidInput, "msg.create-channel", "invalid TTL format")
		}
		ttlSeconds = int64(ttlDuration.Seconds())
	}

	req := &grpc.MessagingCreateChannelRequest{
		Name:           channelName,
		BufferSize:     msgBufferSize,
		MaxSubscribers: msgMaxSubscribers,
		Persistent:     msgPersistent,
		TTLSeconds:     ttlSeconds,
	}

	resp, err := client.MessagingCreateChannel(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatChannelResponse(resp.Channel, msgOutputFmt, os.Stdout)
}

// =============================================================================
// Delete Channel Command
// =============================================================================

// newMsgDeleteChannelCommand creates the delete-channel command.
func newMsgDeleteChannelCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete-channel <name>",
		Short: "Delete a channel and disconnect all subscribers",
		Long: `Delete a channel and disconnect all subscribers.

Examples:
  quicbaas msg delete-channel orders
  quicbaas msg delete-channel orders --force`,
		Args: cobra.ExactArgs(1),
		RunE: runMsgDeleteChannelCommand,
	}

	cmd.Flags().BoolVar(&msgForce, "force", false, "Delete without confirmation")

	return cmd
}

// runMsgDeleteChannelCommand executes the delete-channel command.
func runMsgDeleteChannelCommand(cmd *cobra.Command, args []string) error {
	channelName := args[0]

	// Prompt for confirmation if not forced
	if !msgForce {
		fmt.Printf("Are you sure you want to delete channel '%s'? This will disconnect all subscribers. (y/N): ", channelName)
		var confirm string
		if _, scanErr := fmt.Scanln(&confirm); scanErr != nil {
			return errors.Wrapf(scanErr, errors.ErrInvalidInput, "msg.delete-channel", "failed to read confirmation")
		}
		if !strings.EqualFold(confirm, "y") && !strings.EqualFold(confirm, "yes") {
			fmt.Println("Delete cancelled.")
			return nil
		}
	}

	client, err := messagingClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &grpc.MessagingDeleteChannelRequest{
		Name: channelName,
	}

	resp, err := client.MessagingDeleteChannel(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatDeleteChannelResponse(resp, msgOutputFmt, os.Stdout)
}

// =============================================================================
// List Channels Command
// =============================================================================

// newMsgListChannelsCommand creates the list-channels command.
func newMsgListChannelsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-channels",
		Short: "List all channels with optional filtering",
		Long: `List all channels with optional filtering.

Examples:
  quicbaas msg list-channels
  quicbaas msg list-channels --format json
  quicbaas msg list-channels --filter "order*"
  quicbaas msg list-channels --limit 10 --offset 20`,
		Args: cobra.NoArgs,
		RunE: runMsgListChannelsCommand,
	}

	cmd.Flags().Int32Var(&msgLimit, "limit", 100, "Maximum number of results")
	cmd.Flags().Int32Var(&msgOffset, "offset", 0, "Pagination offset")
	cmd.Flags().StringVar(&msgFilter, "filter", "", "Filter by name pattern (glob)")

	return cmd
}

// runMsgListChannelsCommand executes the list-channels command.
func runMsgListChannelsCommand(cmd *cobra.Command, args []string) error {
	client, err := messagingClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	// Calculate cursor from offset for pagination
	cursor := ""
	if msgOffset > 0 {
		cursor = fmt.Sprintf("%d", msgOffset)
	}

	req := &grpc.MessagingListChannelsRequest{
		Limit:      msgLimit,
		Cursor:     cursor,
		NamePrefix: msgFilter,
	}

	resp, err := client.MessagingListChannels(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatChannelListResponse(resp, msgOutputFmt, os.Stdout)
}

// =============================================================================
// Channel Info Command
// =============================================================================

// newMsgChannelInfoCommand creates the channel-info command.
func newMsgChannelInfoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channel-info <name>",
		Short: "Get detailed information about a channel",
		Long: `Get detailed information about a channel.

Examples:
  quicbaas msg channel-info orders
  quicbaas msg channel-info orders --format yaml`,
		Args: cobra.ExactArgs(1),
		RunE: runMsgChannelInfoCommand,
	}

	return cmd
}

// runMsgChannelInfoCommand executes the channel-info command.
func runMsgChannelInfoCommand(cmd *cobra.Command, args []string) error {
	channelName := args[0]

	client, err := messagingClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	// Get channel info
	channelReq := &grpc.MessagingGetChannelRequest{
		Name: channelName,
	}

	channelResp, err := client.MessagingGetChannel(ctx, channelReq)
	if err != nil {
		return formatError(err)
	}

	// Get channel stats
	statsReq := &grpc.MessagingGetChannelStatsRequest{
		Name: channelName,
	}

	statsResp, statsErr := client.MessagingGetChannelStats(ctx, statsReq)

	// Get dedup stats
	dedupReq := &grpc.MessagingGetDedupStatsRequest{
		Channel: channelName,
	}

	dedupResp, dedupErr := client.MessagingGetDedupStats(ctx, dedupReq)

	return formatChannelInfoResponse(channelResp, statsResp, statsErr, dedupResp, dedupErr, msgOutputFmt, os.Stdout)
}

// =============================================================================
// Subscribe Command
// =============================================================================

// newMsgSubscribeCommand creates the subscribe command.
func newMsgSubscribeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "subscribe <channel>",
		Short: "Subscribe to a channel to receive notifications",
		Long: `Subscribe to a channel to receive notifications.

Examples:
  quicbaas msg subscribe orders
  quicbaas msg subscribe orders --delta
  quicbaas msg subscribe orders --dedup
  quicbaas msg subscribe orders --dedup-skip
  quicbaas msg subscribe orders --filter "status=completed"
  quicbaas msg subscribe orders --replay 1000`,
		Args: cobra.ExactArgs(1),
		RunE: runMsgSubscribeCommand,
	}

	cmd.Flags().BoolVar(&msgDelta, "delta", false, "Enable delta mode (receive only changes)")
	cmd.Flags().BoolVar(&msgDedup, "dedup", false, "Enable deduplication")
	cmd.Flags().BoolVar(&msgDedupSkip, "dedup-skip", false, "Skip duplicate messages silently")
	cmd.Flags().StringVar(&msgFilter, "filter", "", "Message filter expression")
	cmd.Flags().Uint64Var(&msgReplay, "replay", 0, "Replay messages from offset")
	cmd.Flags().Int32Var(&msgBufferSize, "buffer-size", 256, "Client buffer size")

	return cmd
}

// runMsgSubscribeCommand executes the subscribe command.
func runMsgSubscribeCommand(cmd *cobra.Command, args []string) error {
	channelName := args[0]

	client, err := messagingClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithCancel(stdcontext.Background())
	defer cancel()

	// Parse filter into subscription filter
	var filter *grpc.MessagingSubscriptionFilter
	if msgFilter != "" {
		filter = &grpc.MessagingSubscriptionFilter{
			KeyPrefix: msgFilter,
		}
	}

	// Create subscription
	subReq := &grpc.MessagingSubscribeRequest{
		Channel:             channelName,
		Filter:              filter,
		DeltaMode:           msgDelta,
		DedupMetadata:       msgDedup,
		DedupSkipDuplicates: msgDedupSkip,
		BufferSize:          msgBufferSize,
	}

	subResp, err := client.MessagingSubscribe(ctx, subReq)
	if err != nil {
		return formatError(err)
	}

	fmt.Printf("Subscribed to channel: %s\n", channelName)
	fmt.Printf("Subscription ID: %s\n", subResp.Subscription.ID)
	fmt.Println()
	fmt.Println("Waiting for messages (Ctrl+C to exit)...")
	fmt.Println()

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start streaming notifications
	streamReq := &grpc.MessagingStreamNotificationsRequest{
		SubscriptionID: subResp.Subscription.ID,
		StartSequence:  msgReplay,
		IncludeHistory: msgReplay > 0,
	}

	notifChan, errChan, streamErr := client.MessagingStreamNotifications(ctx, streamReq)
	if streamErr != nil {
		return formatError(streamErr)
	}

	// Stream messages until cancelled
	for {
		select {
		case <-sigChan:
			fmt.Println("\nUnsubscribing...")
			unsubReq := &grpc.MessagingUnsubscribeRequest{
				SubscriptionID: subResp.Subscription.ID,
			}
			unsubCtx, unsubCancel := stdcontext.WithTimeout(stdcontext.Background(), 5*time.Second)
			_, unsubErr := client.MessagingUnsubscribe(unsubCtx, unsubReq)
			unsubCancel()
			if unsubErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to unsubscribe: %v\n", unsubErr)
			}
			return nil

		case notif, ok := <-notifChan:
			if !ok {
				return nil
			}
			formatNotification(notif, os.Stdout)

		case streamErr, ok := <-errChan:
			if !ok {
				return nil
			}
			return formatError(streamErr)
		}
	}
}

// =============================================================================
// Unsubscribe Command
// =============================================================================

// newMsgUnsubscribeCommand creates the unsubscribe command.
func newMsgUnsubscribeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unsubscribe <subscription-id>",
		Short: "Remove a subscription",
		Long: `Remove a subscription.

Examples:
  quicbaas msg unsubscribe sub_abc123
  quicbaas msg unsubscribe sub_abc123 --force`,
		Args: cobra.ExactArgs(1),
		RunE: runMsgUnsubscribeCommand,
	}

	cmd.Flags().BoolVar(&msgForce, "force", false, "Unsubscribe without confirmation")

	return cmd
}

// runMsgUnsubscribeCommand executes the unsubscribe command.
func runMsgUnsubscribeCommand(cmd *cobra.Command, args []string) error {
	subscriptionID := args[0]

	// Prompt for confirmation if not forced
	if !msgForce {
		fmt.Printf("Are you sure you want to unsubscribe '%s'? (y/N): ", subscriptionID)
		var confirm string
		if _, scanErr := fmt.Scanln(&confirm); scanErr != nil {
			return errors.Wrapf(scanErr, errors.ErrInvalidInput, "msg.unsubscribe", "failed to read confirmation")
		}
		if !strings.EqualFold(confirm, "y") && !strings.EqualFold(confirm, "yes") {
			fmt.Println("Unsubscribe cancelled.")
			return nil
		}
	}

	client, err := messagingClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &grpc.MessagingUnsubscribeRequest{
		SubscriptionID: subscriptionID,
	}

	resp, err := client.MessagingUnsubscribe(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatUnsubscribeResponse(resp, msgOutputFmt, os.Stdout)
}

// =============================================================================
// List Subscriptions Command
// =============================================================================

// newMsgListSubscriptionsCommand creates the list-subscriptions command.
func newMsgListSubscriptionsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-subscriptions",
		Short: "List active subscriptions",
		Long: `List active subscriptions.

Examples:
  quicbaas msg list-subscriptions
  quicbaas msg list-subscriptions --channel orders
  quicbaas msg list-subscriptions --format json`,
		Args: cobra.NoArgs,
		RunE: runMsgListSubscriptionsCommand,
	}

	cmd.Flags().StringVar(&msgChannel, "channel", "", "Filter by channel name")
	cmd.Flags().Int32Var(&msgLimit, "limit", 100, "Maximum number of results")
	cmd.Flags().Int32Var(&msgOffset, "offset", 0, "Pagination offset")

	return cmd
}

// runMsgListSubscriptionsCommand executes the list-subscriptions command.
func runMsgListSubscriptionsCommand(cmd *cobra.Command, args []string) error {
	// List subscriptions requires getting channel info first, then iterating
	// For now, we'll get the channel stats which includes subscriber count
	client, err := messagingClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	// If a channel is specified, get its info
	if msgChannel != "" {
		statsReq := &grpc.MessagingGetChannelStatsRequest{
			Name: msgChannel,
		}

		statsResp, statsErr := client.MessagingGetChannelStats(ctx, statsReq)
		if statsErr != nil {
			return formatError(statsErr)
		}

		return formatSubscriptionListFromStats(statsResp, msgOutputFmt, os.Stdout)
	}

	// Otherwise, list all channels and their subscription counts
	listReq := &grpc.MessagingListChannelsRequest{
		Limit: msgLimit,
	}

	listResp, listErr := client.MessagingListChannels(ctx, listReq)
	if listErr != nil {
		return formatError(listErr)
	}

	return formatSubscriptionListFromChannels(listResp, msgOutputFmt, os.Stdout)
}

// =============================================================================
// Publish Command
// =============================================================================

// newMsgPublishCommand creates the publish command.
func newMsgPublishCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish <channel>",
		Short: "Publish a message to a channel",
		Long: `Publish a message to a channel.

Examples:
  quicbaas msg publish orders --payload '{"order_id": "ord_001", "status": "created"}'
  quicbaas msg publish orders --dedup-id "order-ord_001-created" --payload '{"order_id": "ord_001"}'
  quicbaas msg publish orders --file /path/to/message.json
  quicbaas msg publish alerts --payload '{"level": "critical"}' --priority high --ttl 5m`,
		Args: cobra.ExactArgs(1),
		RunE: runMsgPublishCommand,
	}

	cmd.Flags().StringVar(&msgPayload, "payload", "", "Message payload as JSON string")
	cmd.Flags().StringVar(&msgFile, "file", "", "Read payload from file")
	cmd.Flags().StringVar(&msgDedupID, "dedup-id", "", "Deduplication ID for idempotent publishing")
	cmd.Flags().StringVar(&msgPriority, "priority", "normal", "Message priority (low, normal, high)")
	cmd.Flags().StringVar(&msgTTL, "ttl", "", "Message time-to-live")
	cmd.Flags().BoolVar(&msgAck, "ack", false, "Wait for delivery acknowledgment")

	return cmd
}

// runMsgPublishCommand executes the publish command.
func runMsgPublishCommand(cmd *cobra.Command, args []string) error {
	channelName := args[0]

	// Get payload from flag or file
	var payload []byte
	var err error

	if msgFile != "" {
		payload, err = os.ReadFile(msgFile)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "msg.publish", "failed to read payload file")
		}
	} else if msgPayload != "" {
		payload = []byte(msgPayload)
	} else {
		return errors.Newf(errors.ErrInvalidInput, "msg.publish", "either --payload or --file must be specified")
	}

	client, err := messagingClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	// Build metadata from priority
	metadata := make(map[string]string)
	if msgPriority != "" {
		metadata["priority"] = msgPriority
	}
	if msgTTL != "" {
		metadata["ttl"] = msgTTL
	}

	req := &grpc.MessagingPublishRequest{
		Channel:  channelName,
		Payload:  payload,
		DedupKey: msgDedupID,
		Metadata: metadata,
	}

	resp, err := client.MessagingPublish(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatPublishResponse(resp, msgOutputFmt, os.Stdout)
}

// =============================================================================
// Publish Delta Command
// =============================================================================

// newMsgPublishDeltaCommand creates the publish-delta command.
func newMsgPublishDeltaCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish-delta <channel>",
		Short: "Publish only the differences between two entity states",
		Long: `Publish only the differences between two entity states.

Examples:
  quicbaas msg publish-delta orders \
    --old '{"order_id": "ord_001", "status": "created"}' \
    --new '{"order_id": "ord_001", "status": "shipped"}'

  quicbaas msg publish-delta orders \
    --old-file /path/to/old-state.json \
    --new-file /path/to/new-state.json

  quicbaas msg publish-delta orders \
    --entity-id "ord_001" \
    --entity-type "Order" \
    --old '{"status": "processing"}' \
    --new '{"status": "shipped", "tracking": "TRK123"}'`,
		Args: cobra.ExactArgs(1),
		RunE: runMsgPublishDeltaCommand,
	}

	cmd.Flags().StringVar(&msgOld, "old", "", "Previous state as JSON string")
	cmd.Flags().StringVar(&msgNew, "new", "", "Current state as JSON string")
	cmd.Flags().StringVar(&msgOldFile, "old-file", "", "Read previous state from file")
	cmd.Flags().StringVar(&msgNewFile, "new-file", "", "Read current state from file")
	cmd.Flags().StringVar(&msgEntityID, "entity-id", "", "Entity identifier")
	cmd.Flags().StringVar(&msgEntityType, "entity-type", "", "Entity type name")
	cmd.Flags().StringVar(&msgDedupID, "dedup-id", "", "Deduplication ID")

	return cmd
}

// runMsgPublishDeltaCommand executes the publish-delta command.
func runMsgPublishDeltaCommand(cmd *cobra.Command, args []string) error {
	channelName := args[0]

	// Get old value from flag or file
	var oldValue []byte
	var err error

	if msgOldFile != "" {
		oldValue, err = os.ReadFile(msgOldFile)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "msg.publish-delta", "failed to read old state file")
		}
	} else if msgOld != "" {
		oldValue = []byte(msgOld)
	} else {
		return errors.Newf(errors.ErrInvalidInput, "msg.publish-delta", "either --old or --old-file must be specified")
	}

	// Get new value from flag or file
	var newValue []byte

	if msgNewFile != "" {
		newValue, err = os.ReadFile(msgNewFile)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "msg.publish-delta", "failed to read new state file")
		}
	} else if msgNew != "" {
		newValue = []byte(msgNew)
	} else {
		return errors.Newf(errors.ErrInvalidInput, "msg.publish-delta", "either --new or --new-file must be specified")
	}

	client, err := messagingClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	// Build metadata from entity info
	metadata := make(map[string]string)
	if msgEntityID != "" {
		metadata["entity_id"] = msgEntityID
	}
	if msgEntityType != "" {
		metadata["entity_type"] = msgEntityType
	}

	req := &grpc.MessagingPublishDeltaRequest{
		Channel:  channelName,
		OldValue: oldValue,
		NewValue: newValue,
		DedupKey: msgDedupID,
		Metadata: metadata,
	}

	resp, err := client.MessagingPublishDelta(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatPublishDeltaResponse(resp, msgOutputFmt, os.Stdout)
}

// =============================================================================
// Watch Command
// =============================================================================

// newMsgWatchCommand creates the watch command.
func newMsgWatchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch <channel>",
		Short: "Watch a channel in real-time and display messages as they arrive",
		Long: `Watch a channel in real-time and display messages as they arrive.

Examples:
  quicbaas msg watch orders
  quicbaas msg watch orders --delta
  quicbaas msg watch orders --filter "status=shipped"
  quicbaas msg watch orders --output /var/log/orders.jsonl
  quicbaas msg watch orders --raw`,
		Args: cobra.ExactArgs(1),
		RunE: runMsgWatchCommand,
	}

	cmd.Flags().BoolVar(&msgDelta, "delta", false, "Enable delta mode")
	cmd.Flags().StringVar(&msgFilter, "filter", "", "Message filter expression")
	cmd.Flags().StringVar(&msgFile, "output", "", "Output file (append mode)")
	cmd.Flags().BoolVar(&msgTimestamps, "timestamps", true, "Show timestamps")
	cmd.Flags().BoolVar(&msgRaw, "raw", false, "Output raw JSON without formatting")

	return cmd
}

// runMsgWatchCommand executes the watch command.
func runMsgWatchCommand(cmd *cobra.Command, args []string) error {
	channelName := args[0]

	client, err := messagingClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithCancel(stdcontext.Background())
	defer cancel()

	// Open output file if specified
	var outputWriter io.Writer = os.Stdout
	if msgFile != "" {
		file, fileErr := os.OpenFile(msgFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if fileErr != nil {
			return errors.Wrapf(fileErr, errors.ErrInvalidInput, "msg.watch", "failed to open output file")
		}
		defer file.Close()
		outputWriter = file
	}

	// Parse filter into subscription filter
	var filter *grpc.MessagingSubscriptionFilter
	if msgFilter != "" {
		filter = &grpc.MessagingSubscriptionFilter{
			KeyPrefix: msgFilter,
		}
	}

	// Create subscription
	subReq := &grpc.MessagingSubscribeRequest{
		Channel:   channelName,
		Filter:    filter,
		DeltaMode: msgDelta,
	}

	subResp, err := client.MessagingSubscribe(ctx, subReq)
	if err != nil {
		return formatError(err)
	}

	modeStr := ""
	if msgDelta {
		modeStr = " (delta mode)"
	}
	fmt.Printf("Watching channel: %s%s (Ctrl+C to exit)\n\n", channelName, modeStr)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start streaming notifications
	streamReq := &grpc.MessagingStreamNotificationsRequest{
		SubscriptionID: subResp.Subscription.ID,
	}

	notifChan, errChan, streamErr := client.MessagingStreamNotifications(ctx, streamReq)
	if streamErr != nil {
		return formatError(streamErr)
	}

	// Stream messages until cancelled
	for {
		select {
		case <-sigChan:
			fmt.Println("\nStopping watch...")
			unsubReq := &grpc.MessagingUnsubscribeRequest{
				SubscriptionID: subResp.Subscription.ID,
			}
			unsubCtx, unsubCancel := stdcontext.WithTimeout(stdcontext.Background(), 5*time.Second)
			_, unsubErr := client.MessagingUnsubscribe(unsubCtx, unsubReq)
			unsubCancel()
			if unsubErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to unsubscribe: %v\n", unsubErr)
			}
			return nil

		case notif, ok := <-notifChan:
			if !ok {
				return nil
			}
			formatWatchNotification(notif, outputWriter, msgTimestamps, msgRaw)

		case watchErr, ok := <-errChan:
			if !ok {
				return nil
			}
			return formatError(watchErr)
		}
	}
}

// =============================================================================
// Dedup Stats Command
// =============================================================================

// newMsgDedupStatsCommand creates the dedup-stats command.
func newMsgDedupStatsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dedup-stats <channel>",
		Short: "Show deduplication statistics for a channel",
		Long: `Show deduplication statistics for a channel.

Examples:
  quicbaas msg dedup-stats orders
  quicbaas msg dedup-stats orders --format json
  quicbaas msg dedup-stats orders --window 1h
  quicbaas msg dedup-stats orders --reset`,
		Args: cobra.ExactArgs(1),
		RunE: runMsgDedupStatsCommand,
	}

	cmd.Flags().BoolVar(&msgReset, "reset", false, "Reset statistics after display")
	cmd.Flags().StringVar(&msgWindow, "window", "", "Show stats for specific time window")

	return cmd
}

// runMsgDedupStatsCommand executes the dedup-stats command.
func runMsgDedupStatsCommand(cmd *cobra.Command, args []string) error {
	channelName := args[0]

	client, err := messagingClientFactory()
	if err != nil {
		return err
	}
	defer client.Close()

	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), timeout)
	defer cancel()

	req := &grpc.MessagingGetDedupStatsRequest{
		Channel: channelName,
	}

	resp, err := client.MessagingGetDedupStats(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatDedupStatsResponse(resp, msgOutputFmt, os.Stdout)
}

// =============================================================================
// Formatting Functions
// =============================================================================

// formatChannelResponse formats a channel for output.
func formatChannelResponse(channel *grpc.MessagingChannel, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(channel)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(w)
		return encoder.Encode(channel)

	case cli.OutputFormatTable:
		fmt.Fprintln(w, "Channel created successfully.")
		fmt.Fprintln(w)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Name:\t%s\n", channel.Name)
		fmt.Fprintf(tw, "Buffer Size:\t%d\n", channel.BufferSize)
		fmt.Fprintf(tw, "Persistent:\t%v\n", channel.Persistent)
		if channel.TTLSeconds > 0 {
			fmt.Fprintf(tw, "TTL:\t%s\n", time.Duration(channel.TTLSeconds)*time.Second)
		} else {
			fmt.Fprintf(tw, "TTL:\t0s (no expiry)\n")
		}
		fmt.Fprintf(tw, "Created:\t%s\n", channel.CreatedAt.Format(time.RFC3339))
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Channel created: %s\n", channel.Name)
		return nil
	}
}

// formatDeleteChannelResponse formats a delete channel response for output.
func formatDeleteChannelResponse(resp *grpc.MessagingDeleteChannelResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(w)
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Channel:\t%s\n", resp.Name)
		fmt.Fprintf(tw, "Deleted:\t%v\n", resp.Deleted)
		return tw.Flush()

	default:
		if resp.Deleted {
			fmt.Fprintf(w, "Channel '%s' deleted successfully\n", resp.Name)
		} else {
			fmt.Fprintf(w, "Channel '%s' was not deleted\n", resp.Name)
		}
		return nil
	}
}

// formatChannelListResponse formats a channel list for output.
func formatChannelListResponse(resp *grpc.MessagingListChannelsResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(w)
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "NAME\tSUBSCRIBERS\tPERSISTENT\tCREATED\n")
		for _, ch := range resp.Channels {
			fmt.Fprintf(tw, "%s\t%d\t%v\t%s\n",
				ch.Name, ch.Subscribers, ch.Persistent, ch.CreatedAt.Format(time.RFC3339))
		}
		fmt.Fprintf(tw, "\nTotal: %d channels\n", resp.TotalCount)
		if resp.HasMore {
			fmt.Fprintf(tw, "More results available. Use --offset %s\n", resp.NextCursor)
		}
		return tw.Flush()

	default:
		for _, ch := range resp.Channels {
			fmt.Fprintf(w, "%s (%d subscribers)\n", ch.Name, ch.Subscribers)
		}
		return nil
	}
}

// formatChannelInfoResponse formats channel info with stats for output.
func formatChannelInfoResponse(
	channelResp *grpc.MessagingGetChannelResponse,
	statsResp *grpc.MessagingGetChannelStatsResponse,
	statsErr error,
	dedupResp *grpc.MessagingGetDedupStatsResponse,
	dedupErr error,
	format string,
	w io.Writer,
) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	// Build combined response for JSON/YAML
	type combinedResponse struct {
		Channel *grpc.MessagingChannel      `json:"channel" yaml:"channel"`
		Stats   *grpc.MessagingChannelStats `json:"stats,omitempty" yaml:"stats,omitempty"`
		Dedup   *grpc.MessagingDedupStats   `json:"dedup,omitempty" yaml:"dedup,omitempty"`
	}

	combined := combinedResponse{
		Channel: channelResp.Channel,
	}
	if statsErr == nil && statsResp != nil {
		combined.Stats = statsResp.Stats
	}
	if dedupErr == nil && dedupResp != nil {
		combined.Dedup = dedupResp.Stats
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(combined)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(w)
		return encoder.Encode(combined)

	case cli.OutputFormatTable:
		ch := channelResp.Channel
		fmt.Fprintf(w, "Channel: %s\n", ch.Name)
		fmt.Fprintln(w, strings.Repeat("=", len("Channel: ")+len(ch.Name)))
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Name:\t%s\n", ch.Name)
		fmt.Fprintf(tw, "Buffer Size:\t%d\n", ch.BufferSize)
		fmt.Fprintf(tw, "Persistent:\t%v\n", ch.Persistent)
		if ch.TTLSeconds > 0 {
			fmt.Fprintf(tw, "TTL:\t%s\n", time.Duration(ch.TTLSeconds)*time.Second)
		} else {
			fmt.Fprintf(tw, "TTL:\t0s (no expiry)\n")
		}
		fmt.Fprintf(tw, "Created:\t%s\n", ch.CreatedAt.Format(time.RFC3339))
		if flushErr := tw.Flush(); flushErr != nil {
			return flushErr
		}

		// Statistics section
		if statsErr == nil && statsResp != nil && statsResp.Stats != nil {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Statistics")
			fmt.Fprintln(w, "----------")
			tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "Active Subscribers:\t%d\n", statsResp.Stats.Subscribers)
			fmt.Fprintf(tw, "Messages Published:\t%d\n", statsResp.Stats.MessagesPublished)
			fmt.Fprintf(tw, "Messages In History:\t%d\n", statsResp.Stats.MessagesInHistory)
			if flushErr := tw.Flush(); flushErr != nil {
				return flushErr
			}
		}

		// Deduplication section
		if dedupErr == nil && dedupResp != nil && dedupResp.Stats != nil {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Deduplication")
			fmt.Fprintln(w, "-------------")
			tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "Total Messages:\t%d\n", dedupResp.Stats.TotalMessages)
			fmt.Fprintf(tw, "Total Duplicates:\t%d\n", dedupResp.Stats.TotalDuplicates)
			if dedupResp.Stats.TotalMessages > 0 {
				rate := float64(dedupResp.Stats.TotalDuplicates) / float64(dedupResp.Stats.TotalMessages) * 100
				fmt.Fprintf(tw, "Duplicate Rate:\t%.1f%%\n", rate)
			}
			if flushErr := tw.Flush(); flushErr != nil {
				return flushErr
			}
		}

		return nil

	default:
		fmt.Fprintf(w, "Channel: %s\n", channelResp.Channel.Name)
		return nil
	}
}

// formatUnsubscribeResponse formats an unsubscribe response for output.
func formatUnsubscribeResponse(resp *grpc.MessagingUnsubscribeResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(w)
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Subscription ID:\t%s\n", resp.SubscriptionID)
		fmt.Fprintf(tw, "Unsubscribed:\t%v\n", resp.Unsubscribed)
		return tw.Flush()

	default:
		if resp.Unsubscribed {
			fmt.Fprintf(w, "Unsubscribed from '%s' successfully\n", resp.SubscriptionID)
		} else {
			fmt.Fprintf(w, "Failed to unsubscribe from '%s'\n", resp.SubscriptionID)
		}
		return nil
	}
}

// formatSubscriptionListFromStats formats subscription list from channel stats.
func formatSubscriptionListFromStats(resp *grpc.MessagingGetChannelStatsResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(w)
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "CHANNEL\tSUBSCRIBERS\n")
		fmt.Fprintf(tw, "%s\t%d\n", resp.Stats.Name, resp.Stats.Subscribers)
		return tw.Flush()

	default:
		fmt.Fprintf(w, "%s: %d subscribers\n", resp.Stats.Name, resp.Stats.Subscribers)
		return nil
	}
}

// formatSubscriptionListFromChannels formats subscription list from channel list.
func formatSubscriptionListFromChannels(resp *grpc.MessagingListChannelsResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(w)
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "CHANNEL\tSUBSCRIBERS\n")
		for _, ch := range resp.Channels {
			fmt.Fprintf(tw, "%s\t%d\n", ch.Name, ch.Subscribers)
		}
		return tw.Flush()

	default:
		for _, ch := range resp.Channels {
			fmt.Fprintf(w, "%s: %d subscribers\n", ch.Name, ch.Subscribers)
		}
		return nil
	}
}

// formatPublishResponse formats a publish response for output.
func formatPublishResponse(resp *grpc.MessagingPublishResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(w)
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		fmt.Fprintln(w, "Message published successfully.")
		fmt.Fprintln(w)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Channel:\t%s\n", resp.Channel)
		fmt.Fprintf(tw, "Message ID:\t%s\n", resp.MessageID)
		fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
		if resp.IsDuplicate {
			fmt.Fprintf(tw, "Duplicate:\tyes\n")
		}
		return tw.Flush()

	default:
		fmt.Fprintf(w, "Published message %s to channel %s\n", resp.MessageID, resp.Channel)
		return nil
	}
}

// formatPublishDeltaResponse formats a publish delta response for output.
func formatPublishDeltaResponse(resp *grpc.MessagingPublishDeltaResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(w)
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		fmt.Fprintln(w, "Delta published successfully.")
		fmt.Fprintln(w)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Channel:\t%s\n", resp.Channel)
		fmt.Fprintf(tw, "Message ID:\t%s\n", resp.MessageID)
		if resp.Delta != nil {
			fmt.Fprintf(tw, "Entity ID:\t%s\n", resp.Delta.EntityID)
			fmt.Fprintf(tw, "Entity Type:\t%s\n", resp.Delta.EntityType)
			fmt.Fprintf(tw, "Version:\t%d\n", resp.Delta.Version)
			fmt.Fprintf(tw, "Changes:\t%d\n", len(resp.Delta.Changes))
		}
		fmt.Fprintf(tw, "Timestamp:\t%s\n", resp.Timestamp.Format(time.RFC3339))
		if flushErr := tw.Flush(); flushErr != nil {
			return flushErr
		}

		// Print field changes
		if resp.Delta != nil && len(resp.Delta.Changes) > 0 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Field Changes:")
			for _, change := range resp.Delta.Changes {
				oldVal := "(none)"
				if len(change.OldValue) > 0 {
					oldVal = string(change.OldValue)
				}
				newVal := "(none)"
				if len(change.Value) > 0 {
					newVal = string(change.Value)
				}
				fmt.Fprintf(w, "  - %s: %s -> %s\n", change.Path, oldVal, newVal)
			}
		}

		return nil

	default:
		fmt.Fprintf(w, "Published delta %s to channel %s\n", resp.MessageID, resp.Channel)
		return nil
	}
}

// formatDedupStatsResponse formats dedup stats for output.
func formatDedupStatsResponse(resp *grpc.MessagingGetDedupStatsResponse, format string, w io.Writer) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)

	case cli.OutputFormatYAML:
		encoder := yaml.NewEncoder(w)
		return encoder.Encode(resp)

	case cli.OutputFormatTable:
		stats := resp.Stats
		fmt.Fprintf(w, "Deduplication Statistics: %s\n", stats.Channel)
		fmt.Fprintln(w, strings.Repeat("=", len("Deduplication Statistics: ")+len(stats.Channel)))
		fmt.Fprintln(w)

		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Total Messages:\t%d\n", stats.TotalMessages)
		uniqueMessages := stats.TotalMessages - stats.TotalDuplicates
		fmt.Fprintf(tw, "Unique Messages:\t%d\n", uniqueMessages)
		fmt.Fprintf(tw, "Duplicates:\t%d", stats.TotalDuplicates)
		if stats.TotalMessages > 0 {
			rate := float64(stats.TotalDuplicates) / float64(stats.TotalMessages) * 100
			fmt.Fprintf(tw, " (%.1f%%)", rate)
		}
		fmt.Fprintln(tw)
		if flushErr := tw.Flush(); flushErr != nil {
			return flushErr
		}

		// Print top duplicates if available
		if len(stats.TopDuplicates) > 0 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Top Duplicate Keys")
			fmt.Fprintln(w, "------------------")
			tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			fmt.Fprintf(tw, "KEY\tCOUNT\tLAST SEEN\n")
			for _, dup := range stats.TopDuplicates {
				key := dup.MessageID
				if len(key) > 40 {
					key = key[:37] + "..."
				}
				fmt.Fprintf(tw, "%s\t%d\t%s\n", key, dup.Count, dup.LastSeen.Format(time.RFC3339))
			}
			if flushErr := tw.Flush(); flushErr != nil {
				return flushErr
			}
		}

		return nil

	default:
		fmt.Fprintf(w, "%s: %d messages, %d duplicates\n",
			resp.Stats.Channel, resp.Stats.TotalMessages, resp.Stats.TotalDuplicates)
		return nil
	}
}

// formatNotification formats a notification for subscribe output.
func formatNotification(notif *grpc.MessagingNotification, w io.Writer) {
	if notif == nil || notif.Message == nil {
		return
	}

	msg := notif.Message
	timestamp := msg.Timestamp.Format(time.RFC3339)
	payload := string(msg.Payload)

	fmt.Fprintf(w, "[%s] %s\n", timestamp, payload)
}

// formatWatchNotification formats a notification for watch output.
func formatWatchNotification(notif *grpc.MessagingNotification, w io.Writer, showTimestamps bool, raw bool) {
	if notif == nil || notif.Message == nil {
		return
	}

	msg := notif.Message

	if raw {
		// Raw JSON output
		data, marshalErr := json.Marshal(msg)
		if marshalErr != nil {
			fmt.Fprintf(w, "Error: %v\n", marshalErr)
			return
		}
		fmt.Fprintln(w, string(data))
		return
	}

	// Formatted output
	if showTimestamps {
		fmt.Fprintf(w, "[%s] ", msg.Timestamp.Format(time.RFC3339))
	}

	// Try to pretty-print JSON payload
	var prettyPayload interface{}
	if jsonErr := json.Unmarshal(msg.Payload, &prettyPayload); jsonErr == nil {
		// Successfully parsed as JSON, pretty print
		prettyBytes, prettyErr := json.MarshalIndent(prettyPayload, "  ", "  ")
		if prettyErr == nil {
			fmt.Fprintln(w, "Message received")
			fmt.Fprintf(w, "  %s\n", string(prettyBytes))
			return
		}
	}

	// Fallback to raw payload
	fmt.Fprintf(w, "%s\n", string(msg.Payload))
}
