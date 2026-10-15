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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
)

// Messaging command flags.
var (
	// create-channel and subscribe flags.
	msgBufferSize     int
	msgMaxSubscribers int
	msgPersistent     bool
	msgChannelTTL     string

	// delete-channel and unsubscribe flags.
	msgForce bool

	// subscribe and watch flags.
	msgFilter    string
	msgDelta     bool
	msgDedup     bool
	msgDedupSkip bool

	// publish flags.
	msgPayload    string
	msgFile       string
	msgDedupID    string
	msgPriority   string
	msgPublishTTL string

	// publish-delta flags.
	msgOld        string
	msgNew        string
	msgOldFile    string
	msgNewFile    string
	msgEntityID   string
	msgEntityType string

	// list-subscriptions flags.
	msgChannel string

	// Output format (persistent flag).
	msgOutputFmt string
)

// msgDeleteChannelResult is printed after a channel is deleted.
type msgDeleteChannelResult struct {
	Channel string
	Deleted bool
}

// msgUnsubscribeResult is printed after a subscription is removed.
type msgUnsubscribeResult struct {
	SubscriptionID string
	Unsubscribed   bool
}

// msgPublishResult is printed after a message or delta is published.
type msgPublishResult struct {
	Channel   string
	Published bool
}

// msgChannelInfo combines a channel with its delivery and dedup statistics.
type msgChannelInfo struct {
	Channel *messaging.Channel
	Stats   *messaging.ChannelStats
	Dedup   *messaging.DedupStats
}

// msgChannelSubscribers is one row of the list-subscriptions output.
type msgChannelSubscribers struct {
	Channel     string
	Subscribers int64
}

// messagingSDKClient returns the messaging client of the shared SDK client.
// The SDK client is cached by the factory, so callers do not close it.
func messagingSDKClient() (messaging.Client, error) {
	client, err := sdkClientFactory()
	if err != nil {
		return nil, err
	}
	return client.Async().Messaging(), nil
}

// newMsgCommands creates the msg command group.
func newMsgCommands() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "msg",
		Short: "Messaging commands (channels, subscriptions, publishing)",
		Long: `Messaging commands for managing notification channels and subscriptions.

These commands create and inspect channels, create and remove subscriptions,
and publish messages in QuicBaaS.`,
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

	// Reject an unknown --output before any command changes server state.
	for _, sub := range cmd.Commands() {
		sub.PreRunE = validateMsgOutputFormat
	}

	return cmd
}

// validateMsgOutputFormat checks --output so a command never creates,
// publishes or deletes and then fails while printing the result.
func validateMsgOutputFormat(_ *cobra.Command, _ []string) error {
	_, err := cli.ParseOutputFormat(msgOutputFmt)
	return err
}

// =============================================================================
// create-channel
// =============================================================================

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

	cmd.Flags().IntVar(&msgBufferSize, "buffer-size", 256, "Channel buffer size")
	cmd.Flags().IntVar(&msgMaxSubscribers, "max-subscribers", 1000, "Maximum subscribers allowed")
	cmd.Flags().BoolVar(&msgPersistent, "persistent", false, "Enable message persistence")
	cmd.Flags().StringVar(&msgChannelTTL, "ttl", "0s", "Message time-to-live")

	return cmd
}

func runMsgCreateChannelCommand(cmd *cobra.Command, args []string) error {
	ttl, err := time.ParseDuration(msgChannelTTL)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "msg.create-channel", "invalid TTL format")
	}

	client, err := messagingSDKClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	channel, err := client.CreateChannel(ctx, &messaging.CreateChannelRequest{
		Name:       args[0],
		BufferSize: msgBufferSize,
		MaxSubs:    msgMaxSubscribers,
		Persistent: msgPersistent,
		TTL:        ttl,
	})
	if err != nil {
		return formatError(err)
	}

	return formatChannelResponse(channel, msgOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// delete-channel
// =============================================================================

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

func runMsgDeleteChannelCommand(cmd *cobra.Command, args []string) error {
	channelName := args[0]

	if !msgForce {
		prompt := fmt.Sprintf("Are you sure you want to delete channel '%s'? This will disconnect all subscribers. (y/N): ", channelName)
		confirmed, err := confirmMsgAction(cmd, prompt, "msg.delete-channel")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(cmd.OutOrStdout(), "Delete cancelled.")
			return nil
		}
	}

	client, err := messagingSDKClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	if err := client.DeleteChannel(ctx, channelName); err != nil {
		return formatError(err)
	}

	result := &msgDeleteChannelResult{Channel: channelName, Deleted: true}
	return writeMsgOutput(cmd.OutOrStdout(), msgOutputFmt, result, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "Channel '%s' deleted successfully.\n", channelName)
		return err
	})
}

// =============================================================================
// list-channels
// =============================================================================

func newMsgListChannelsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list-channels",
		Short: "List all channels",
		Long: `List all channels.

Examples:
  quicbaas msg list-channels
  quicbaas msg list-channels --output json`,
		Args: cobra.NoArgs,
		RunE: runMsgListChannelsCommand,
	}
}

func runMsgListChannelsCommand(cmd *cobra.Command, args []string) error {
	client, err := messagingSDKClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	resp, err := client.ListChannels(ctx)
	if err != nil {
		return formatError(err)
	}

	return formatChannelListResponse(resp, msgOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// channel-info
// =============================================================================

func newMsgChannelInfoCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "channel-info <name>",
		Short: "Get detailed information about a channel",
		Long: `Get detailed information about a channel, including its delivery
and deduplication statistics.

Examples:
  quicbaas msg channel-info orders
  quicbaas msg channel-info orders --output yaml`,
		Args: cobra.ExactArgs(1),
		RunE: runMsgChannelInfoCommand,
	}
}

func runMsgChannelInfoCommand(cmd *cobra.Command, args []string) error {
	channelName := args[0]

	client, err := messagingSDKClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	channel, err := client.GetChannel(ctx, channelName)
	if err != nil {
		return formatError(err)
	}
	stats, err := client.GetChannelStats(ctx, channelName)
	if err != nil {
		return formatError(err)
	}
	dedup, err := client.GetDedupStats(ctx, channelName)
	if err != nil {
		return formatError(err)
	}

	info := &msgChannelInfo{Channel: channel, Stats: stats, Dedup: dedup}
	return formatChannelInfoResponse(info, msgOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// subscribe
// =============================================================================

func newMsgSubscribeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "subscribe <channel>",
		Short: "Create a subscription on a channel",
		Long: `Create a subscription on a channel and print it.

The command registers the subscription and exits; it does not stream messages.

Examples:
  quicbaas msg subscribe orders
  quicbaas msg subscribe orders --delta
  quicbaas msg subscribe orders --dedup
  quicbaas msg subscribe orders --dedup-skip
  quicbaas msg subscribe orders --filter "orders/eu/"`,
		Args: cobra.ExactArgs(1),
		RunE: runMsgSubscribeCommand,
	}

	cmd.Flags().BoolVar(&msgDelta, "delta", false, "Enable delta mode (receive only changes)")
	cmd.Flags().BoolVar(&msgDedup, "dedup", false, "Include deduplication metadata")
	cmd.Flags().BoolVar(&msgDedupSkip, "dedup-skip", false, "Skip duplicate messages silently")
	cmd.Flags().StringVar(&msgFilter, "filter", "", "Only deliver messages whose key has this prefix")
	cmd.Flags().IntVar(&msgBufferSize, "buffer-size", 256, "Subscription buffer size")

	return cmd
}

func runMsgSubscribeCommand(cmd *cobra.Command, args []string) error {
	client, err := messagingSDKClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	req := &messaging.SubscribeRequest{
		Channel:             args[0],
		DeltaMode:           msgDelta,
		DedupMetadata:       msgDedup,
		DedupSkipDuplicates: msgDedupSkip,
		BufferSize:          msgBufferSize,
	}
	if msgFilter != "" {
		req.Filter = &messaging.SubscriptionFilter{KeyPrefix: msgFilter}
	}

	sub, err := client.Subscribe(ctx, req)
	if err != nil {
		return formatError(err)
	}

	return formatSubscriptionResponse(sub, msgOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// unsubscribe
// =============================================================================

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

func runMsgUnsubscribeCommand(cmd *cobra.Command, args []string) error {
	subscriptionID := args[0]

	if !msgForce {
		prompt := fmt.Sprintf("Are you sure you want to unsubscribe '%s'? (y/N): ", subscriptionID)
		confirmed, err := confirmMsgAction(cmd, prompt, "msg.unsubscribe")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(cmd.OutOrStdout(), "Unsubscribe cancelled.")
			return nil
		}
	}

	client, err := messagingSDKClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	if err := client.Unsubscribe(ctx, subscriptionID); err != nil {
		return formatError(err)
	}

	result := &msgUnsubscribeResult{SubscriptionID: subscriptionID, Unsubscribed: true}
	return writeMsgOutput(cmd.OutOrStdout(), msgOutputFmt, result, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "Subscription '%s' removed successfully.\n", subscriptionID)
		return err
	})
}

// =============================================================================
// list-subscriptions
// =============================================================================

func newMsgListSubscriptionsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list-subscriptions",
		Short: "List subscriber counts per channel",
		Long: `List the number of active subscribers on each channel.

Examples:
  quicbaas msg list-subscriptions
  quicbaas msg list-subscriptions --channel orders
  quicbaas msg list-subscriptions --output json`,
		Args: cobra.NoArgs,
		RunE: runMsgListSubscriptionsCommand,
	}

	cmd.Flags().StringVar(&msgChannel, "channel", "", "Show only this channel")

	return cmd
}

func runMsgListSubscriptionsCommand(cmd *cobra.Command, args []string) error {
	client, err := messagingSDKClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	var rows []msgChannelSubscribers
	if msgChannel != "" {
		stats, err := client.GetChannelStats(ctx, msgChannel)
		if err != nil {
			return formatError(err)
		}
		rows = append(rows, msgChannelSubscribers{Channel: stats.Name, Subscribers: stats.Subscribers})
	} else {
		resp, err := client.ListChannels(ctx)
		if err != nil {
			return formatError(err)
		}
		for _, ch := range resp.Channels {
			rows = append(rows, msgChannelSubscribers{Channel: ch.Name, Subscribers: ch.Subscribers})
		}
	}

	return formatSubscriptionList(rows, msgOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// publish
// =============================================================================

func newMsgPublishCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish <channel>",
		Short: "Publish a message to a channel",
		Long: `Publish a message to a channel.

--priority and --ttl are sent as message metadata (keys "priority" and "ttl").

Examples:
  quicbaas msg publish orders --payload '{"order_id": "ord_001", "status": "created"}'
  quicbaas msg publish orders --dedup-id "order-ord_001-created" --payload '{"order_id": "ord_001"}'
  quicbaas msg publish orders --file ./message.json
  quicbaas msg publish alerts --payload '{"level": "critical"}' --priority high --ttl 5m`,
		Args: cobra.ExactArgs(1),
		RunE: runMsgPublishCommand,
	}

	cmd.Flags().StringVar(&msgPayload, "payload", "", "Message payload")
	cmd.Flags().StringVar(&msgFile, "file", "", "Read payload from file")
	cmd.Flags().StringVar(&msgDedupID, "dedup-id", "", "Deduplication key for idempotent publishing")
	cmd.Flags().StringVar(&msgPriority, "priority", "normal", "Priority metadata (low, normal, high)")
	cmd.Flags().StringVar(&msgPublishTTL, "ttl", "", "Time-to-live metadata")

	return cmd
}

func runMsgPublishCommand(cmd *cobra.Command, args []string) error {
	channelName := args[0]

	var payload []byte
	switch {
	case msgFile != "":
		data, err := os.ReadFile(msgFile)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInvalidInput, "msg.publish", "failed to read payload file")
		}
		payload = data
	case msgPayload != "":
		payload = []byte(msgPayload)
	default:
		return errors.Newf(errors.ErrInvalidInput, "msg.publish", "either --payload or --file must be specified")
	}

	metadata := make(map[string]string)
	if msgPriority != "" {
		metadata["priority"] = msgPriority
	}
	if msgPublishTTL != "" {
		metadata["ttl"] = msgPublishTTL
	}

	client, err := messagingSDKClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	err = client.Publish(ctx, &messaging.PublishRequest{
		Channel:  channelName,
		Payload:  payload,
		DedupKey: msgDedupID,
		Metadata: metadata,
	})
	if err != nil {
		return formatError(err)
	}

	return formatPublishResponse(&msgPublishResult{Channel: channelName, Published: true}, msgOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// publish-delta
// =============================================================================

func newMsgPublishDeltaCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish-delta <channel>",
		Short: "Publish only the differences between two entity states",
		Long: `Publish only the differences between two entity states.

Both states must be JSON. --entity-id and --entity-type are sent as message
metadata (keys "entity_id" and "entity_type").

Examples:
  quicbaas msg publish-delta orders \
    --old '{"order_id": "ord_001", "status": "created"}' \
    --new '{"order_id": "ord_001", "status": "shipped"}'

  quicbaas msg publish-delta orders \
    --old-file ./old-state.json \
    --new-file ./new-state.json

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
	cmd.Flags().StringVar(&msgEntityID, "entity-id", "", "Entity identifier metadata")
	cmd.Flags().StringVar(&msgEntityType, "entity-type", "", "Entity type metadata")
	cmd.Flags().StringVar(&msgDedupID, "dedup-id", "", "Deduplication key")

	return cmd
}

func runMsgPublishDeltaCommand(cmd *cobra.Command, args []string) error {
	channelName := args[0]

	oldState, err := readMsgDeltaState(msgOld, msgOldFile, "old")
	if err != nil {
		return err
	}
	newState, err := readMsgDeltaState(msgNew, msgNewFile, "new")
	if err != nil {
		return err
	}

	metadata := make(map[string]string)
	if msgEntityID != "" {
		metadata["entity_id"] = msgEntityID
	}
	if msgEntityType != "" {
		metadata["entity_type"] = msgEntityType
	}

	client, err := messagingSDKClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	err = client.PublishDelta(ctx, &messaging.PublishDeltaRequest{
		Channel:  channelName,
		Old:      oldState,
		New:      newState,
		DedupKey: msgDedupID,
		Metadata: metadata,
	})
	if err != nil {
		return formatError(err)
	}

	return formatPublishResponse(&msgPublishResult{Channel: channelName, Published: true}, msgOutputFmt, cmd.OutOrStdout())
}

// readMsgDeltaState decodes one publish-delta state from its file flag or,
// failing that, its inline flag. name is "old" or "new".
func readMsgDeltaState(inline, file, name string) (interface{}, error) {
	var data []byte
	switch {
	case file != "":
		fileData, err := os.ReadFile(file)
		if err != nil {
			return nil, errors.Wrapf(err, errors.ErrInvalidInput, "msg.publish-delta", "failed to read "+name+" state file")
		}
		data = fileData
	case inline != "":
		data = []byte(inline)
	default:
		return nil, errors.Newf(errors.ErrInvalidInput, "msg.publish-delta", "either --%s or --%s-file must be specified", name, name)
	}

	// UseNumber keeps numbers as their literal text, so a large integer is
	// forwarded as written rather than rounded through float64.
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var state interface{}
	if err := dec.Decode(&state); err != nil {
		return nil, errors.Wrapf(err, errors.ErrInvalidInput, "msg.publish-delta", name+" state is not valid JSON")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.Newf(errors.ErrInvalidInput, "msg.publish-delta", "%s state has data after its JSON value", name)
	}
	return state, nil
}

// =============================================================================
// watch
// =============================================================================

func newMsgWatchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch <key-prefix>",
		Short: "Create a subscription on KV changes under a key prefix",
		Long: `Create a subscription on KV changes under a key prefix and print it.

The command registers the watch and exits; it does not stream messages.
The REST and QUIC transports do not serve watch and return an error.

Examples:
  quicbaas msg watch orders/
  quicbaas msg watch orders/ --delta`,
		Args: cobra.ExactArgs(1),
		RunE: runMsgWatchCommand,
	}

	cmd.Flags().BoolVar(&msgDelta, "delta", false, "Enable delta mode")

	return cmd
}

func runMsgWatchCommand(cmd *cobra.Command, args []string) error {
	client, err := messagingSDKClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	sub, err := client.WatchChannel(ctx, &messaging.WatchChannelRequest{
		KeyPrefix: args[0],
		DeltaMode: msgDelta,
	})
	if err != nil {
		return formatError(err)
	}

	return formatSubscriptionResponse(sub, msgOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// dedup-stats
// =============================================================================

func newMsgDedupStatsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "dedup-stats <channel>",
		Short: "Show deduplication statistics for a channel",
		Long: `Show deduplication statistics for a channel.

Examples:
  quicbaas msg dedup-stats orders
  quicbaas msg dedup-stats orders --output json`,
		Args: cobra.ExactArgs(1),
		RunE: runMsgDedupStatsCommand,
	}
}

func runMsgDedupStatsCommand(cmd *cobra.Command, args []string) error {
	channelName := args[0]

	client, err := messagingSDKClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	stats, err := client.GetDedupStats(ctx, channelName)
	if err != nil {
		return formatError(err)
	}

	return formatDedupStatsResponse(channelName, stats, msgOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// Helpers and formatters
// =============================================================================

// confirmMsgAction prints prompt and reads a y/N answer from the command input.
func confirmMsgAction(cmd *cobra.Command, prompt, op string) (bool, error) {
	fmt.Fprint(cmd.OutOrStdout(), prompt)
	var answer string
	if _, err := fmt.Fscanln(cmd.InOrStdin(), &answer); err != nil {
		return false, errors.Wrapf(err, errors.ErrInvalidInput, op, "failed to read confirmation")
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}

// writeMsgOutput encodes v as JSON or YAML, or calls table for table output.
func writeMsgOutput(w io.Writer, format string, v interface{}, table func(io.Writer) error) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}

	switch outputFormat {
	case cli.OutputFormatJSON:
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(v)
	case cli.OutputFormatYAML:
		return yaml.NewEncoder(w).Encode(v)
	default:
		return table(w)
	}
}

// formatMsgTTL renders a channel TTL, spelling out that zero means no expiry.
func formatMsgTTL(ttl time.Duration) string {
	if ttl <= 0 {
		return "0s (no expiry)"
	}
	return ttl.String()
}

// dedupRate returns duplicates as a percentage of checks.
func dedupRate(stats *messaging.DedupStats) float64 {
	if stats.TotalChecks == 0 {
		return 0
	}
	return float64(stats.DuplicatesDetected) / float64(stats.TotalChecks) * 100
}

func formatChannelResponse(channel *messaging.Channel, format string, w io.Writer) error {
	return writeMsgOutput(w, format, channel, func(w io.Writer) error {
		fmt.Fprintln(w, "Channel created successfully.")
		fmt.Fprintln(w)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Name:\t%s\n", channel.Name)
		fmt.Fprintf(tw, "Buffer Size:\t%d\n", channel.BufferSize)
		fmt.Fprintf(tw, "Persistent:\t%v\n", channel.Persistent)
		fmt.Fprintf(tw, "TTL:\t%s\n", formatMsgTTL(channel.TTL))
		fmt.Fprintf(tw, "Created:\t%s\n", channel.CreatedAt.Format(time.RFC3339))
		return tw.Flush()
	})
}

func formatChannelListResponse(resp *messaging.ListChannelsResponse, format string, w io.Writer) error {
	return writeMsgOutput(w, format, resp, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tSUBSCRIBERS\tPERSISTENT\tCREATED")
		for _, ch := range resp.Channels {
			fmt.Fprintf(tw, "%s\t%d\t%v\t%s\n", ch.Name, ch.Subscribers, ch.Persistent, ch.CreatedAt.Format(time.RFC3339))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(w)
		_, err := fmt.Fprintf(w, "Total: %d channels\n", len(resp.Channels))
		return err
	})
}

func formatChannelInfoResponse(info *msgChannelInfo, format string, w io.Writer) error {
	return writeMsgOutput(w, format, info, func(w io.Writer) error {
		header := "Channel: " + info.Channel.Name
		fmt.Fprintln(w, header)
		fmt.Fprintln(w, strings.Repeat("=", len(header)))
		fmt.Fprintln(w)

		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Codec:\t%s\n", info.Channel.Codec)
		fmt.Fprintf(tw, "Buffer Size:\t%d\n", info.Channel.BufferSize)
		fmt.Fprintf(tw, "Max History:\t%d\n", info.Channel.MaxHistory)
		fmt.Fprintf(tw, "Persistent:\t%v\n", info.Channel.Persistent)
		fmt.Fprintf(tw, "TTL:\t%s\n", formatMsgTTL(info.Channel.TTL))
		fmt.Fprintf(tw, "Created:\t%s\n", info.Channel.CreatedAt.Format(time.RFC3339))
		fmt.Fprintln(tw)
		fmt.Fprintln(tw, "Statistics")
		fmt.Fprintf(tw, "  Active Subscribers:\t%d\n", info.Stats.Subscribers)
		fmt.Fprintf(tw, "  Messages Published:\t%d\n", info.Stats.MessagesPublished)
		fmt.Fprintf(tw, "  Messages In History:\t%d\n", info.Stats.MessagesInHistory)
		fmt.Fprintln(tw)
		fmt.Fprintln(tw, "Deduplication")
		fmt.Fprintf(tw, "  Total Checks:\t%d\n", info.Dedup.TotalChecks)
		fmt.Fprintf(tw, "  Duplicates Detected:\t%d\n", info.Dedup.DuplicatesDetected)
		fmt.Fprintf(tw, "  Duplicate Rate:\t%.2f%%\n", dedupRate(info.Dedup))
		return tw.Flush()
	})
}

func formatSubscriptionResponse(sub *messaging.Subscription, format string, w io.Writer) error {
	return writeMsgOutput(w, format, sub, func(w io.Writer) error {
		fmt.Fprintln(w, "Subscription created successfully.")
		fmt.Fprintln(w)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Subscription ID:\t%s\n", sub.ID)
		if sub.Channel != "" {
			fmt.Fprintf(tw, "Channel:\t%s\n", sub.Channel)
		}
		if sub.ClientID != "" {
			fmt.Fprintf(tw, "Client ID:\t%s\n", sub.ClientID)
		}
		if sub.Filters != nil && sub.Filters.KeyPrefix != "" {
			fmt.Fprintf(tw, "Key Prefix:\t%s\n", sub.Filters.KeyPrefix)
		}
		fmt.Fprintf(tw, "Created:\t%s\n", sub.CreatedAt.Format(time.RFC3339))
		return tw.Flush()
	})
}

func formatSubscriptionList(rows []msgChannelSubscribers, format string, w io.Writer) error {
	return writeMsgOutput(w, format, rows, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "CHANNEL\tSUBSCRIBERS")
		for _, row := range rows {
			fmt.Fprintf(tw, "%s\t%d\n", row.Channel, row.Subscribers)
		}
		return tw.Flush()
	})
}

func formatPublishResponse(result *msgPublishResult, format string, w io.Writer) error {
	return writeMsgOutput(w, format, result, func(w io.Writer) error {
		_, err := fmt.Fprintf(w, "Message published to channel '%s'.\n", result.Channel)
		return err
	})
}

func formatDedupStatsResponse(channelName string, stats *messaging.DedupStats, format string, w io.Writer) error {
	return writeMsgOutput(w, format, stats, func(w io.Writer) error {
		header := "Deduplication Statistics: " + channelName
		fmt.Fprintln(w, header)
		fmt.Fprintln(w, strings.Repeat("=", len(header)))
		fmt.Fprintln(w)

		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "Total Checks:\t%d\n", stats.TotalChecks)
		fmt.Fprintf(tw, "Unique Messages:\t%d\n", stats.UniqueMessages)
		fmt.Fprintf(tw, "Duplicates Detected:\t%d (%.2f%%)\n", stats.DuplicatesDetected, dedupRate(stats))
		fmt.Fprintf(tw, "Cache Size:\t%d\n", stats.CacheSize)
		fmt.Fprintf(tw, "Window:\t%s\n", stats.WindowDuration)
		return tw.Flush()
	})
}
