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
	stderrors "errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	pkgmessaging "github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
)

// msgCommandError is returned by the failing SDK client factory.
type msgCommandError struct{}

func (msgCommandError) Error() string { return "sdk client unavailable" }

// withEmbeddedMessaging points the CLI at the real SDK client over the
// embedded transport, backed by a running in-memory push service.
func withEmbeddedMessaging(t *testing.T) {
	t.Helper()

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "console", Output: "stdout"})
	require.NoError(t, err)
	ps := pkgmessaging.NewPushService(log, nil)
	require.NoError(t, ps.Start(context.Background()))

	svc := &testutil.MockQuicBaaS{}
	svc.On("Messaging").Return(ps)
	client, err := quicbaas.NewClient("embedded", quicbaas.WithEmbedded(svc))
	require.NoError(t, err)

	origFactory, origTimeout := sdkClientFactory, globalTimeout
	sdkClientFactory = func() (quicbaas.Client, error) { return client, nil }
	globalTimeout = 10 * time.Second
	t.Cleanup(func() {
		sdkClientFactory, globalTimeout = origFactory, origTimeout
		if err := client.Close(); err != nil {
			t.Errorf("close client: %v", err)
		}
		if err := ps.Stop(context.Background()); err != nil {
			t.Errorf("stop push service: %v", err)
		}
	})
}

// assertInvalidInput asserts that err carries the ErrInvalidInput code.
func assertInvalidInput(t *testing.T, err error, msgAndArgs ...interface{}) {
	t.Helper()
	var qbErr *errors.QBError
	if assert.True(t, stderrors.As(err, &qbErr), msgAndArgs...) {
		assert.Equal(t, errors.ErrInvalidInput, qbErr.Code, msgAndArgs...)
	}
}

// runMsg executes the msg command group with args and stdin, returning stdout.
func runMsg(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := newMsgCommands()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// mustRunMsg runs a msg command that must succeed.
func mustRunMsg(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runMsg(t, "", args...)
	require.NoError(t, err, "msg %v", args)
	return out
}

// msgChannelInfoJSON reads channel-info as JSON.
func msgChannelInfoJSON(t *testing.T, name string) msgChannelInfo {
	t.Helper()
	var info msgChannelInfo
	require.NoError(t, json.Unmarshal([]byte(mustRunMsg(t, "channel-info", name, "--output", "json")), &info))
	return info
}

func TestMsgCommands_Subcommands(t *testing.T) {
	cmd := newMsgCommands()
	var names []string
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	assert.ElementsMatch(t, []string{
		"create-channel", "delete-channel", "list-channels", "channel-info",
		"subscribe", "unsubscribe", "list-subscriptions", "publish",
		"publish-delta", "watch", "dedup-stats",
	}, names)
	assert.Equal(t, "table", cmd.PersistentFlags().Lookup("output").DefValue)
}

func TestMsgCommands_Flags(t *testing.T) {
	cmd := newMsgCommands()
	want := map[string][]string{
		"create-channel":     {"buffer-size", "max-subscribers", "persistent", "ttl"},
		"delete-channel":     {"force"},
		"subscribe":          {"delta", "dedup", "dedup-skip", "filter", "buffer-size"},
		"unsubscribe":        {"force"},
		"list-subscriptions": {"channel"},
		"publish":            {"payload", "file", "dedup-id", "priority", "ttl"},
		"publish-delta":      {"old", "new", "old-file", "new-file", "entity-id", "entity-type", "dedup-id"},
		"watch":              {"delta"},
	}
	for _, sub := range cmd.Commands() {
		var got []string
		sub.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) { got = append(got, f.Name) })
		assert.ElementsMatch(t, want[sub.Name()], got, "flags of %s", sub.Name())
	}
}

func TestMsgCommands_ArgCounts(t *testing.T) {
	withEmbeddedMessaging(t)
	for _, args := range [][]string{
		{"create-channel"}, {"delete-channel"}, {"channel-info"}, {"subscribe"},
		{"unsubscribe"}, {"publish"}, {"publish-delta"}, {"watch"}, {"dedup-stats"},
		{"list-channels", "extra"}, {"list-subscriptions", "extra"},
	} {
		_, err := runMsg(t, "", args...)
		assert.Error(t, err, "msg %v", args)
	}
}

func TestMsgCommands_FactoryError(t *testing.T) {
	origFactory := sdkClientFactory
	sdkClientFactory = func() (quicbaas.Client, error) { return nil, msgCommandError{} }
	t.Cleanup(func() { sdkClientFactory = origFactory })

	for _, args := range [][]string{
		{"create-channel", "orders"},
		{"delete-channel", "orders", "--force"},
		{"list-channels"},
		{"channel-info", "orders"},
		{"subscribe", "orders"},
		{"unsubscribe", "sub-1", "--force"},
		{"list-subscriptions"},
		{"publish", "orders", "--payload", "x"},
		{"publish-delta", "orders", "--old", "{}", "--new", "{}"},
		{"watch", "orders/"},
		{"dedup-stats", "orders"},
	} {
		_, err := runMsg(t, "", args...)
		assert.ErrorIs(t, err, msgCommandError{}, "msg %v", args)
	}
}

func TestMsgCreateChannel_CreatesChannelWithFlags(t *testing.T) {
	withEmbeddedMessaging(t)

	out := mustRunMsg(t, "create-channel", "orders", "--buffer-size", "512", "--persistent", "--ttl", "1h")
	assert.Contains(t, out, "Channel created successfully.")
	assert.Contains(t, out, "orders")
	assert.Contains(t, out, "512")
	assert.Contains(t, out, "1h0m0s")

	info := msgChannelInfoJSON(t, "orders")
	assert.Equal(t, "orders", info.Channel.Name)
	assert.Equal(t, 512, info.Channel.BufferSize)
	assert.True(t, info.Channel.Persistent)
	assert.Equal(t, time.Hour, info.Channel.TTL)
}

func TestMsgCreateChannel_OutputFormats(t *testing.T) {
	withEmbeddedMessaging(t)

	var channel messaging.Channel
	require.NoError(t, json.Unmarshal([]byte(mustRunMsg(t, "create-channel", "a", "--output", "json")), &channel))
	assert.Equal(t, "a", channel.Name)

	var doc map[string]interface{}
	require.NoError(t, yaml.Unmarshal([]byte(mustRunMsg(t, "create-channel", "b", "--output", "yaml")), &doc))
	assert.Equal(t, "b", doc["name"])

	out := mustRunMsg(t, "create-channel", "c")
	assert.Contains(t, out, "0s (no expiry)")
}

func TestMsgCreateChannel_InvalidInput(t *testing.T) {
	withEmbeddedMessaging(t)

	_, err := runMsg(t, "", "create-channel", "orders", "--ttl", "soon")
	assertInvalidInput(t, err, "got %v", err)

	_, err = runMsg(t, "", "create-channel", "orders", "--buffer-size", "-1")
	assert.Error(t, err)

	_, err = runMsg(t, "", "create-channel", "orders", "--output", "xml")
	assert.Error(t, err)

	// None of the rejected requests may leave a channel behind.
	_, err = runMsg(t, "", "channel-info", "orders")
	assert.Error(t, err)
}

func TestMsgDeleteChannel_Confirmation(t *testing.T) {
	withEmbeddedMessaging(t)
	mustRunMsg(t, "create-channel", "orders")

	out, err := runMsg(t, "n\n", "delete-channel", "orders")
	require.NoError(t, err)
	assert.Contains(t, out, "Delete cancelled.")
	mustRunMsg(t, "channel-info", "orders")

	_, err = runMsg(t, "", "delete-channel", "orders")
	assertInvalidInput(t, err, "unanswered prompt: %v", err)
	mustRunMsg(t, "channel-info", "orders")

	out, err = runMsg(t, "yes\n", "delete-channel", "orders")
	require.NoError(t, err)
	assert.Contains(t, out, "Channel 'orders' deleted successfully.")
	_, err = runMsg(t, "", "channel-info", "orders")
	assert.Error(t, err)
}

func TestMsgDeleteChannel_ForceAndErrors(t *testing.T) {
	withEmbeddedMessaging(t)
	mustRunMsg(t, "create-channel", "orders")

	var result msgDeleteChannelResult
	require.NoError(t, json.Unmarshal([]byte(mustRunMsg(t, "delete-channel", "orders", "--force", "--output", "json")), &result))
	assert.Equal(t, msgDeleteChannelResult{Channel: "orders", Deleted: true}, result)

	_, err := runMsg(t, "", "delete-channel", "orders", "--force")
	assert.Error(t, err, "deleting a missing channel")
}

func TestMsgListChannels(t *testing.T) {
	withEmbeddedMessaging(t)

	out := mustRunMsg(t, "list-channels")
	assert.Contains(t, out, "Total: 0 channels")

	mustRunMsg(t, "create-channel", "orders")
	mustRunMsg(t, "create-channel", "events", "--persistent")

	out = mustRunMsg(t, "list-channels")
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "orders")
	assert.Contains(t, out, "events")
	assert.Contains(t, out, "Total: 2 channels")

	var resp messaging.ListChannelsResponse
	require.NoError(t, json.Unmarshal([]byte(mustRunMsg(t, "list-channels", "--output", "json")), &resp))
	assert.Len(t, resp.Channels, 2)

	_, err := runMsg(t, "", "list-channels", "--output", "xml")
	assert.Error(t, err)
}

func TestMsgChannelInfo(t *testing.T) {
	withEmbeddedMessaging(t)
	mustRunMsg(t, "create-channel", "orders")
	mustRunMsg(t, "subscribe", "orders")
	mustRunMsg(t, "publish", "orders", "--payload", "one")

	out := mustRunMsg(t, "channel-info", "orders")
	assert.Contains(t, out, "Channel: orders")
	assert.Contains(t, out, "Statistics")
	assert.Contains(t, out, "Deduplication")

	info := msgChannelInfoJSON(t, "orders")
	assert.Equal(t, int64(1), info.Stats.Subscribers)
	assert.Equal(t, int64(1), info.Stats.MessagesPublished)
	require.NotNil(t, info.Dedup)

	_, err := runMsg(t, "", "channel-info", "missing")
	assert.Error(t, err)
}

func TestMsgSubscribe(t *testing.T) {
	withEmbeddedMessaging(t)
	mustRunMsg(t, "create-channel", "orders")

	out := mustRunMsg(t, "subscribe", "orders")
	assert.Contains(t, out, "Subscription created successfully.")
	assert.Contains(t, out, "Subscription ID:")

	var sub messaging.Subscription
	require.NoError(t, json.Unmarshal([]byte(mustRunMsg(t, "subscribe", "orders",
		"--delta", "--dedup", "--dedup-skip", "--filter", "orders/eu/", "--buffer-size", "64",
		"--output", "json")), &sub))
	assert.NotEmpty(t, sub.ID)
	assert.Equal(t, "orders", sub.Channel)

	assert.Equal(t, int64(2), msgChannelInfoJSON(t, "orders").Stats.Subscribers)

	_, err := runMsg(t, "", "subscribe", "orders", "--buffer-size", "-1")
	assert.Error(t, err)
}

func TestMsgUnsubscribe(t *testing.T) {
	withEmbeddedMessaging(t)
	mustRunMsg(t, "create-channel", "orders")

	var sub messaging.Subscription
	require.NoError(t, json.Unmarshal([]byte(mustRunMsg(t, "subscribe", "orders", "--output", "json")), &sub))

	out, err := runMsg(t, "no\n", "unsubscribe", sub.ID)
	require.NoError(t, err)
	assert.Contains(t, out, "Unsubscribe cancelled.")
	assert.Equal(t, int64(1), msgChannelInfoJSON(t, "orders").Stats.Subscribers)

	_, err = runMsg(t, "", "unsubscribe", sub.ID)
	assertInvalidInput(t, err, "unanswered prompt: %v", err)

	out, err = runMsg(t, "y\n", "unsubscribe", sub.ID)
	require.NoError(t, err)
	assert.Contains(t, out, "removed successfully")
	assert.Equal(t, int64(0), msgChannelInfoJSON(t, "orders").Stats.Subscribers)

	require.NoError(t, json.Unmarshal([]byte(mustRunMsg(t, "subscribe", "orders", "--output", "json")), &sub))
	var result msgUnsubscribeResult
	require.NoError(t, json.Unmarshal([]byte(mustRunMsg(t, "unsubscribe", sub.ID, "--force", "--output", "json")), &result))
	assert.Equal(t, msgUnsubscribeResult{SubscriptionID: sub.ID, Unsubscribed: true}, result)

	_, err = runMsg(t, "", "unsubscribe", "no-such-subscription", "--force")
	assert.Error(t, err)
}

func TestMsgListSubscriptions(t *testing.T) {
	withEmbeddedMessaging(t)
	mustRunMsg(t, "create-channel", "orders")
	mustRunMsg(t, "create-channel", "events")
	mustRunMsg(t, "subscribe", "orders")
	mustRunMsg(t, "subscribe", "orders")

	out := mustRunMsg(t, "list-subscriptions")
	assert.Contains(t, out, "CHANNEL")
	assert.Contains(t, out, "events")

	var rows []msgChannelSubscribers
	require.NoError(t, json.Unmarshal([]byte(mustRunMsg(t, "list-subscriptions", "--output", "json")), &rows))
	assert.ElementsMatch(t, []msgChannelSubscribers{{Channel: "orders", Subscribers: 2}, {Channel: "events", Subscribers: 0}}, rows)

	require.NoError(t, json.Unmarshal([]byte(mustRunMsg(t, "list-subscriptions", "--channel", "orders", "--output", "json")), &rows))
	assert.Equal(t, []msgChannelSubscribers{{Channel: "orders", Subscribers: 2}}, rows)

	_, err := runMsg(t, "", "list-subscriptions", "--channel", "missing")
	assert.Error(t, err)
}

func TestMsgPublish(t *testing.T) {
	withEmbeddedMessaging(t)
	mustRunMsg(t, "create-channel", "orders")

	out := mustRunMsg(t, "publish", "orders", "--payload", `{"id":1}`, "--priority", "high", "--ttl", "5m")
	assert.Contains(t, out, "Message published to channel 'orders'.")

	path := filepath.Join(t.TempDir(), "message.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"id":2}`), 0o600))
	var result msgPublishResult
	require.NoError(t, json.Unmarshal([]byte(mustRunMsg(t, "publish", "orders", "--file", path, "--output", "json")), &result))
	assert.Equal(t, msgPublishResult{Channel: "orders", Published: true}, result)

	assert.Equal(t, int64(2), msgChannelInfoJSON(t, "orders").Stats.MessagesPublished)
}

func TestMsgPublish_InvalidInput(t *testing.T) {
	withEmbeddedMessaging(t)
	mustRunMsg(t, "create-channel", "orders")

	_, err := runMsg(t, "", "publish", "orders")
	assertInvalidInput(t, err, "no payload: %v", err)

	_, err = runMsg(t, "", "publish", "orders", "--file", filepath.Join(t.TempDir(), "missing.json"))
	assertInvalidInput(t, err, "missing file: %v", err)

	_, err = runMsg(t, "", "publish", "missing", "--payload", "x")
	assert.Error(t, err)

	assert.Equal(t, int64(0), msgChannelInfoJSON(t, "orders").Stats.MessagesPublished)
}

func TestMsgPublishDelta(t *testing.T) {
	withEmbeddedMessaging(t)
	mustRunMsg(t, "create-channel", "orders")

	out := mustRunMsg(t, "publish-delta", "orders",
		"--old", `{"status":"created"}`, "--new", `{"status":"shipped"}`,
		"--entity-id", "ord_001", "--entity-type", "Order", "--dedup-id", "d1")
	assert.Contains(t, out, "Message published to channel 'orders'.")

	dir := t.TempDir()
	oldPath, newPath := filepath.Join(dir, "old.json"), filepath.Join(dir, "new.json")
	require.NoError(t, os.WriteFile(oldPath, []byte(`{"n":1}`), 0o600))
	require.NoError(t, os.WriteFile(newPath, []byte(`{"n":2}`), 0o600))
	mustRunMsg(t, "publish-delta", "orders", "--old-file", oldPath, "--new-file", newPath)

	assert.Equal(t, int64(2), msgChannelInfoJSON(t, "orders").Stats.MessagesPublished)
}

func TestMsgPublishDelta_InvalidInput(t *testing.T) {
	withEmbeddedMessaging(t)
	mustRunMsg(t, "create-channel", "orders")
	missing := filepath.Join(t.TempDir(), "missing.json")

	for name, args := range map[string][]string{
		"no old":       {"--new", "{}"},
		"no new":       {"--old", "{}"},
		"old not json": {"--old", "{", "--new", "{}"},
		"new not json": {"--old", "{}", "--new", "not json"},
		"old file":     {"--old-file", missing, "--new", "{}"},
		"new file":     {"--old", "{}", "--new-file", missing},
		"old trailing": {"--old", "{} {}", "--new", "{}"},
	} {
		_, err := runMsg(t, "", append([]string{"publish-delta", "orders"}, args...)...)
		assertInvalidInput(t, err, "%s: %v", name, err)
	}

	assert.Equal(t, int64(0), msgChannelInfoJSON(t, "orders").Stats.MessagesPublished)
}

// TestReadMsgDeltaState_KeepsNumberPrecision pins that a state is forwarded
// without float64 rounding: 2^53+1 must not become 2^53.
func TestReadMsgDeltaState_KeepsNumberPrecision(t *testing.T) {
	const state = `{"id":9007199254740993,"ratio":0.1,"tags":[1,2]}`

	got, err := readMsgDeltaState(state, "", "old")
	require.NoError(t, err)

	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	assert.JSONEq(t, state, string(encoded))
	assert.Contains(t, string(encoded), "9007199254740993")
}

func TestMsgWatch(t *testing.T) {
	withEmbeddedMessaging(t)

	out := mustRunMsg(t, "watch", "orders/")
	assert.Contains(t, out, "Subscription created successfully.")

	var sub messaging.Subscription
	require.NoError(t, json.Unmarshal([]byte(mustRunMsg(t, "watch", "orders/", "--delta", "--output", "json")), &sub))
	assert.NotEmpty(t, sub.ID)

	_, err := runMsg(t, "", "watch", "")
	assert.Error(t, err, "empty key prefix")
}

func TestMsgDedupStats(t *testing.T) {
	withEmbeddedMessaging(t)
	mustRunMsg(t, "create-channel", "orders")

	out := mustRunMsg(t, "dedup-stats", "orders")
	assert.Contains(t, out, "Deduplication Statistics: orders")
	assert.Contains(t, out, "Duplicates Detected:")

	// The in-memory push service runs without a dedup store, so every
	// counter is zero; the formatter test below covers non-zero values.
	var stats messaging.DedupStats
	require.NoError(t, json.Unmarshal([]byte(mustRunMsg(t, "dedup-stats", "orders", "--output", "json")), &stats))
	assert.Equal(t, messaging.DedupStats{}, stats)

	var doc map[string]interface{}
	require.NoError(t, yaml.Unmarshal([]byte(mustRunMsg(t, "dedup-stats", "orders", "--output", "yaml")), &doc))
	assert.Contains(t, doc, "totalchecks")

	_, err := runMsg(t, "", "dedup-stats", "missing")
	assert.Error(t, err)
}

func TestFormatDedupStatsResponse(t *testing.T) {
	stats := &messaging.DedupStats{
		TotalChecks:        8,
		DuplicatesDetected: 2,
		UniqueMessages:     6,
		CacheSize:          6,
		WindowDuration:     5 * time.Minute,
	}

	var table bytes.Buffer
	require.NoError(t, formatDedupStatsResponse("orders", stats, "table", &table))
	out := table.String()
	assert.Contains(t, out, "Deduplication Statistics: orders")
	assert.Contains(t, out, "2 (25.00%)")
	assert.Contains(t, out, "5m0s")

	var encoded bytes.Buffer
	require.NoError(t, formatDedupStatsResponse("orders", stats, "json", &encoded))
	var decoded messaging.DedupStats
	require.NoError(t, json.Unmarshal(encoded.Bytes(), &decoded))
	assert.Equal(t, *stats, decoded)

	assert.Error(t, formatDedupStatsResponse("orders", stats, "xml", &encoded))
}

func TestDedupRate(t *testing.T) {
	assert.Zero(t, dedupRate(&messaging.DedupStats{}))
	assert.InDelta(t, 25.0, dedupRate(&messaging.DedupStats{TotalChecks: 4, DuplicatesDetected: 1}), 0.001)
}

func TestFormatMsgTTL(t *testing.T) {
	assert.Equal(t, "0s (no expiry)", formatMsgTTL(0))
	assert.Equal(t, "0s (no expiry)", formatMsgTTL(-time.Second))
	assert.Equal(t, "1m30s", formatMsgTTL(90*time.Second))
}
