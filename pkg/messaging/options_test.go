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

package messaging

import (
	"testing"
	"time"
)

func TestServiceOptions(t *testing.T) {
	t.Parallel()

	t.Run("WithMaxSubscriptions sets max subscriptions", func(t *testing.T) {
		t.Parallel()
		opts := ApplyServiceOptions(WithMaxSubscriptions(50))
		if opts.maxSubscriptions != 50 {
			t.Errorf("expected maxSubscriptions 50, got %d", opts.maxSubscriptions)
		}
	})

	t.Run("WithMaxSubscriptions ignores negative values", func(t *testing.T) {
		t.Parallel()
		opts := ApplyServiceOptions(WithMaxSubscriptions(-1))
		if opts.maxSubscriptions != DefaultMaxSubscriptions {
			t.Errorf("expected default maxSubscriptions %d, got %d", DefaultMaxSubscriptions, opts.maxSubscriptions)
		}
	})

	t.Run("WithMaxSubscriptions allows zero", func(t *testing.T) {
		t.Parallel()
		opts := ApplyServiceOptions(WithMaxSubscriptions(0))
		if opts.maxSubscriptions != 0 {
			t.Errorf("expected maxSubscriptions 0, got %d", opts.maxSubscriptions)
		}
	})

	t.Run("WithDedup enables dedup", func(t *testing.T) {
		t.Parallel()
		opts := ApplyServiceOptions(WithDedup(true))
		if !opts.dedupEnabled {
			t.Error("expected dedupEnabled to be true")
		}
	})

	t.Run("WithDedup disables dedup", func(t *testing.T) {
		t.Parallel()
		opts := ApplyServiceOptions(WithDedup(true), WithDedup(false))
		if opts.dedupEnabled {
			t.Error("expected dedupEnabled to be false")
		}
	})

	t.Run("WithDedupWindow sets dedup window", func(t *testing.T) {
		t.Parallel()
		opts := ApplyServiceOptions(WithDedupWindow(time.Hour))
		if opts.dedupWindow != time.Hour {
			t.Errorf("expected dedupWindow %v, got %v", time.Hour, opts.dedupWindow)
		}
	})

	t.Run("WithDedupWindow ignores zero", func(t *testing.T) {
		t.Parallel()
		opts := ApplyServiceOptions(WithDedupWindow(0))
		if opts.dedupWindow != DefaultDedupWindow {
			t.Errorf("expected default dedupWindow %v, got %v", DefaultDedupWindow, opts.dedupWindow)
		}
	})

	t.Run("WithDedupWindow ignores negative", func(t *testing.T) {
		t.Parallel()
		opts := ApplyServiceOptions(WithDedupWindow(-time.Minute))
		if opts.dedupWindow != DefaultDedupWindow {
			t.Errorf("expected default dedupWindow %v, got %v", DefaultDedupWindow, opts.dedupWindow)
		}
	})
}

func TestCreateChannelOptions(t *testing.T) {
	t.Parallel()

	t.Run("WithChannelBufferSize sets buffer size", func(t *testing.T) {
		t.Parallel()
		opts := ApplyCreateChannelOptions(WithChannelBufferSize(512))
		if opts.bufferSize != 512 {
			t.Errorf("expected bufferSize 512, got %d", opts.bufferSize)
		}
	})

	t.Run("WithChannelBufferSize ignores zero", func(t *testing.T) {
		t.Parallel()
		opts := ApplyCreateChannelOptions(WithChannelBufferSize(0))
		if opts.bufferSize != DefaultChannelBufferSize {
			t.Errorf("expected default bufferSize %d, got %d", DefaultChannelBufferSize, opts.bufferSize)
		}
	})

	t.Run("WithChannelBufferSize ignores negative", func(t *testing.T) {
		t.Parallel()
		opts := ApplyCreateChannelOptions(WithChannelBufferSize(-1))
		if opts.bufferSize != DefaultChannelBufferSize {
			t.Errorf("expected default bufferSize %d, got %d", DefaultChannelBufferSize, opts.bufferSize)
		}
	})

	t.Run("WithChannelMaxSubscribers sets max subscribers", func(t *testing.T) {
		t.Parallel()
		opts := ApplyCreateChannelOptions(WithChannelMaxSubscribers(100))
		if opts.maxSubs != 100 {
			t.Errorf("expected maxSubs 100, got %d", opts.maxSubs)
		}
	})

	t.Run("WithChannelMaxSubscribers allows zero", func(t *testing.T) {
		t.Parallel()
		opts := ApplyCreateChannelOptions(WithChannelMaxSubscribers(0))
		if opts.maxSubs != 0 {
			t.Errorf("expected maxSubs 0, got %d", opts.maxSubs)
		}
	})

	t.Run("WithChannelMaxSubscribers ignores negative", func(t *testing.T) {
		t.Parallel()
		defaultOpts := defaultCreateChannelOptions()
		opts := ApplyCreateChannelOptions(WithChannelMaxSubscribers(-1))
		if opts.maxSubs != defaultOpts.maxSubs {
			t.Errorf("expected default maxSubs %d, got %d", defaultOpts.maxSubs, opts.maxSubs)
		}
	})

	t.Run("WithChannelTTL sets TTL", func(t *testing.T) {
		t.Parallel()
		opts := ApplyCreateChannelOptions(WithChannelTTL(time.Hour))
		if opts.ttl != time.Hour {
			t.Errorf("expected ttl %v, got %v", time.Hour, opts.ttl)
		}
	})

	t.Run("WithChannelTTL allows zero", func(t *testing.T) {
		t.Parallel()
		opts := ApplyCreateChannelOptions(WithChannelTTL(0))
		if opts.ttl != 0 {
			t.Errorf("expected ttl 0, got %v", opts.ttl)
		}
	})

	t.Run("WithChannelTTL ignores negative", func(t *testing.T) {
		t.Parallel()
		defaultOpts := defaultCreateChannelOptions()
		opts := ApplyCreateChannelOptions(WithChannelTTL(-time.Minute))
		if opts.ttl != defaultOpts.ttl {
			t.Errorf("expected default ttl %v, got %v", defaultOpts.ttl, opts.ttl)
		}
	})
}

func TestSubscribeOptions(t *testing.T) {
	t.Parallel()

	t.Run("WithSession sets session ID", func(t *testing.T) {
		t.Parallel()
		opts := ApplySubscribeOptions(WithSession("session-123"))
		if opts.sessionID != "session-123" {
			t.Errorf("expected sessionID 'session-123', got %q", opts.sessionID)
		}
	})

	t.Run("WithSession ignores empty string", func(t *testing.T) {
		t.Parallel()
		opts := ApplySubscribeOptions(WithSession(""))
		if opts.sessionID != "" {
			t.Errorf("expected empty sessionID, got %q", opts.sessionID)
		}
	})

	t.Run("WithDeltaMode enables delta mode", func(t *testing.T) {
		t.Parallel()
		opts := ApplySubscribeOptions(WithDeltaMode(true))
		if !opts.deltaMode {
			t.Error("expected deltaMode to be true")
		}
	})

	t.Run("WithDeltaMode disables delta mode", func(t *testing.T) {
		t.Parallel()
		opts := ApplySubscribeOptions(WithDeltaMode(false))
		if opts.deltaMode {
			t.Error("expected deltaMode to be false")
		}
	})

	t.Run("WithIncludeOldValues enables old values", func(t *testing.T) {
		t.Parallel()
		opts := ApplySubscribeOptions(WithIncludeOldValues(true))
		if !opts.includeOldValues {
			t.Error("expected includeOldValues to be true")
		}
	})

	t.Run("WithDedupMetadata enables dedup metadata", func(t *testing.T) {
		t.Parallel()
		opts := ApplySubscribeOptions(WithDedupMetadata(true))
		if !opts.dedupMetadata {
			t.Error("expected dedupMetadata to be true")
		}
	})

	t.Run("WithDedupSkipDuplicates enables skip duplicates", func(t *testing.T) {
		t.Parallel()
		opts := ApplySubscribeOptions(WithDedupSkipDuplicates(true))
		if !opts.dedupSkipDuplicates {
			t.Error("expected dedupSkipDuplicates to be true")
		}
	})

	t.Run("WithSubscriptionBufferSize sets buffer size", func(t *testing.T) {
		t.Parallel()
		opts := ApplySubscribeOptions(WithSubscriptionBufferSize(512))
		if opts.bufferSize != 512 {
			t.Errorf("expected bufferSize 512, got %d", opts.bufferSize)
		}
	})

	t.Run("WithSubscriptionBufferSize ignores zero", func(t *testing.T) {
		t.Parallel()
		opts := ApplySubscribeOptions(WithSubscriptionBufferSize(0))
		if opts.bufferSize != DefaultChannelBufferSize {
			t.Errorf("expected default bufferSize %d, got %d", DefaultChannelBufferSize, opts.bufferSize)
		}
	})

	t.Run("WithSubscriptionBufferSize ignores negative", func(t *testing.T) {
		t.Parallel()
		opts := ApplySubscribeOptions(WithSubscriptionBufferSize(-1))
		if opts.bufferSize != DefaultChannelBufferSize {
			t.Errorf("expected default bufferSize %d, got %d", DefaultChannelBufferSize, opts.bufferSize)
		}
	})
}

func TestPublishOptions(t *testing.T) {
	t.Parallel()

	t.Run("WithMessageID sets message ID", func(t *testing.T) {
		t.Parallel()
		opts := ApplyPublishOptions(WithMessageID("msg-123"))
		if opts.messageID != "msg-123" {
			t.Errorf("expected messageID 'msg-123', got %q", opts.messageID)
		}
	})

	t.Run("WithMessageID ignores empty string", func(t *testing.T) {
		t.Parallel()
		opts := ApplyPublishOptions(WithMessageID(""))
		if opts.messageID != "" {
			t.Errorf("expected empty messageID, got %q", opts.messageID)
		}
	})

	t.Run("WithDedupKey sets dedup key", func(t *testing.T) {
		t.Parallel()
		opts := ApplyPublishOptions(WithDedupKey("key-123"))
		if opts.dedupKey != "key-123" {
			t.Errorf("expected dedupKey 'key-123', got %q", opts.dedupKey)
		}
	})

	t.Run("WithDedupKey ignores empty string", func(t *testing.T) {
		t.Parallel()
		opts := ApplyPublishOptions(WithDedupKey(""))
		if opts.dedupKey != "" {
			t.Errorf("expected empty dedupKey, got %q", opts.dedupKey)
		}
	})

	t.Run("WithPublishCodec sets codec", func(t *testing.T) {
		t.Parallel()
		opts := ApplyPublishOptions(WithPublishCodec(CodecMsgpack))
		if opts.codec != CodecMsgpack {
			t.Errorf("expected codec %q, got %q", CodecMsgpack, opts.codec)
		}
	})

	t.Run("WithPublishCodec ignores empty string", func(t *testing.T) {
		t.Parallel()
		opts := ApplyPublishOptions(WithPublishCodec(""))
		if opts.codec != "" {
			t.Errorf("expected empty codec, got %q", opts.codec)
		}
	})

	t.Run("WithMetadata sets metadata", func(t *testing.T) {
		t.Parallel()
		meta := map[string]string{"key1": "value1", "key2": "value2"}
		opts := ApplyPublishOptions(WithMetadata(meta))
		if opts.metadata["key1"] != "value1" {
			t.Errorf("expected metadata key1='value1', got %q", opts.metadata["key1"])
		}
		if opts.metadata["key2"] != "value2" {
			t.Errorf("expected metadata key2='value2', got %q", opts.metadata["key2"])
		}
	})

	t.Run("WithMetadataKV adds key-value pair", func(t *testing.T) {
		t.Parallel()
		opts := ApplyPublishOptions(WithMetadataKV("key", "value"))
		if opts.metadata["key"] != "value" {
			t.Errorf("expected metadata key='value', got %q", opts.metadata["key"])
		}
	})

	t.Run("WithMetadataKV creates map if nil", func(t *testing.T) {
		t.Parallel()
		opts := ApplyPublishOptions(WithMetadataKV("key1", "value1"), WithMetadataKV("key2", "value2"))
		if opts.metadata["key1"] != "value1" {
			t.Errorf("expected metadata key1='value1', got %q", opts.metadata["key1"])
		}
		if opts.metadata["key2"] != "value2" {
			t.Errorf("expected metadata key2='value2', got %q", opts.metadata["key2"])
		}
	})
}

func TestApplyFunctions(t *testing.T) {
	t.Parallel()

	t.Run("ApplyServiceOptions with multiple options", func(t *testing.T) {
		t.Parallel()
		opts := ApplyServiceOptions(
			WithServiceBufferSize(2048),
			WithMaxSubscriptions(50),
			WithDedup(true),
			WithDedupWindow(time.Hour),
			WithDefaultCodec(CodecMsgpack),
		)

		if opts.bufferSize != 2048 {
			t.Errorf("expected bufferSize 2048, got %d", opts.bufferSize)
		}
		if opts.maxSubscriptions != 50 {
			t.Errorf("expected maxSubscriptions 50, got %d", opts.maxSubscriptions)
		}
		if !opts.dedupEnabled {
			t.Error("expected dedupEnabled to be true")
		}
		if opts.dedupWindow != time.Hour {
			t.Errorf("expected dedupWindow %v, got %v", time.Hour, opts.dedupWindow)
		}
		if opts.defaultCodec != CodecMsgpack {
			t.Errorf("expected defaultCodec %q, got %q", CodecMsgpack, opts.defaultCodec)
		}
	})

	t.Run("ApplyCreateChannelOptions with multiple options", func(t *testing.T) {
		t.Parallel()
		opts := ApplyCreateChannelOptions(
			WithCodec(CodecJSON),
			WithPersistence(true),
			WithMaxHistory(100),
			WithChannelBufferSize(512),
			WithChannelMaxSubscribers(50),
			WithChannelTTL(time.Hour),
		)

		if opts.codec != CodecJSON {
			t.Errorf("expected codec %q, got %q", CodecJSON, opts.codec)
		}
		if !opts.persistent {
			t.Error("expected persistent to be true")
		}
		if opts.maxHistory != 100 {
			t.Errorf("expected maxHistory 100, got %d", opts.maxHistory)
		}
		if opts.bufferSize != 512 {
			t.Errorf("expected bufferSize 512, got %d", opts.bufferSize)
		}
		if opts.maxSubs != 50 {
			t.Errorf("expected maxSubs 50, got %d", opts.maxSubs)
		}
		if opts.ttl != time.Hour {
			t.Errorf("expected ttl %v, got %v", time.Hour, opts.ttl)
		}
	})

	t.Run("ApplySubscribeOptions with multiple options", func(t *testing.T) {
		t.Parallel()
		filter := &SubscriptionFilter{EventTypes: []EventType{EventTypePut}}
		opts := ApplySubscribeOptions(
			WithFilter(filter),
			WithSession("session-id"),
			WithDeltaMode(true),
			WithIncludeOldValues(true),
			WithDedupMetadata(true),
			WithDedupSkipDuplicates(true),
			WithSubscriptionBufferSize(1024),
		)

		if opts.filter != filter {
			t.Error("expected filter to be set")
		}
		if opts.sessionID != "session-id" {
			t.Errorf("expected sessionID 'session-id', got %q", opts.sessionID)
		}
		if !opts.deltaMode {
			t.Error("expected deltaMode to be true")
		}
		if !opts.includeOldValues {
			t.Error("expected includeOldValues to be true")
		}
		if !opts.dedupMetadata {
			t.Error("expected dedupMetadata to be true")
		}
		if !opts.dedupSkipDuplicates {
			t.Error("expected dedupSkipDuplicates to be true")
		}
		if opts.bufferSize != 1024 {
			t.Errorf("expected bufferSize 1024, got %d", opts.bufferSize)
		}
	})

	t.Run("ApplyPublishOptions with multiple options", func(t *testing.T) {
		t.Parallel()
		meta := map[string]string{"existing": "value"}
		opts := ApplyPublishOptions(
			WithMessageID("msg-id"),
			WithDedupKey("dedup-key"),
			WithPublishCodec(CodecMsgpack),
			WithMetadata(meta),
			WithMetadataKV("added", "kv"),
		)

		if opts.messageID != "msg-id" {
			t.Errorf("expected messageID 'msg-id', got %q", opts.messageID)
		}
		if opts.dedupKey != "dedup-key" {
			t.Errorf("expected dedupKey 'dedup-key', got %q", opts.dedupKey)
		}
		if opts.codec != CodecMsgpack {
			t.Errorf("expected codec %q, got %q", CodecMsgpack, opts.codec)
		}
		if opts.metadata["existing"] != "value" {
			t.Errorf("expected metadata existing='value', got %q", opts.metadata["existing"])
		}
		if opts.metadata["added"] != "kv" {
			t.Errorf("expected metadata added='kv', got %q", opts.metadata["added"])
		}
	})
}

func TestDefaultOptionValues(t *testing.T) {
	t.Parallel()

	t.Run("defaultServiceOptions has correct values", func(t *testing.T) {
		t.Parallel()
		opts := defaultServiceOptions()

		if opts.bufferSize != DefaultServiceBufferSize {
			t.Errorf("expected bufferSize %d, got %d", DefaultServiceBufferSize, opts.bufferSize)
		}
		if opts.maxSubscriptions != DefaultMaxSubscriptions {
			t.Errorf("expected maxSubscriptions %d, got %d", DefaultMaxSubscriptions, opts.maxSubscriptions)
		}
		if opts.dedupEnabled {
			t.Error("expected dedupEnabled to be false")
		}
		if opts.dedupWindow != DefaultDedupWindow {
			t.Errorf("expected dedupWindow %v, got %v", DefaultDedupWindow, opts.dedupWindow)
		}
		if opts.defaultCodec != DefaultCodec {
			t.Errorf("expected defaultCodec %q, got %q", DefaultCodec, opts.defaultCodec)
		}
	})

	t.Run("defaultCreateChannelOptions has correct values", func(t *testing.T) {
		t.Parallel()
		opts := defaultCreateChannelOptions()

		if opts.codec != "" {
			t.Errorf("expected empty codec, got %q", opts.codec)
		}
		if opts.persistent {
			t.Error("expected persistent to be false")
		}
		if opts.maxHistory != 0 {
			t.Errorf("expected maxHistory 0, got %d", opts.maxHistory)
		}
		if opts.bufferSize != DefaultChannelBufferSize {
			t.Errorf("expected bufferSize %d, got %d", DefaultChannelBufferSize, opts.bufferSize)
		}
		if opts.maxSubs != 0 {
			t.Errorf("expected maxSubs 0, got %d", opts.maxSubs)
		}
		if opts.ttl != 0 {
			t.Errorf("expected ttl 0, got %v", opts.ttl)
		}
	})

	t.Run("defaultSubscribeOptions has correct values", func(t *testing.T) {
		t.Parallel()
		opts := defaultSubscribeOptions()

		if opts.filter != nil {
			t.Error("expected nil filter")
		}
		if opts.sessionID != "" {
			t.Errorf("expected empty sessionID, got %q", opts.sessionID)
		}
		if opts.deltaMode {
			t.Error("expected deltaMode to be false")
		}
		if opts.includeOldValues {
			t.Error("expected includeOldValues to be false")
		}
		if opts.dedupMetadata {
			t.Error("expected dedupMetadata to be false")
		}
		if opts.dedupSkipDuplicates {
			t.Error("expected dedupSkipDuplicates to be false")
		}
		if opts.bufferSize != DefaultChannelBufferSize {
			t.Errorf("expected bufferSize %d, got %d", DefaultChannelBufferSize, opts.bufferSize)
		}
	})

	t.Run("defaultPublishOptions has correct values", func(t *testing.T) {
		t.Parallel()
		opts := defaultPublishOptions()

		if opts.messageID != "" {
			t.Errorf("expected empty messageID, got %q", opts.messageID)
		}
		if opts.dedupKey != "" {
			t.Errorf("expected empty dedupKey, got %q", opts.dedupKey)
		}
		if opts.codec != "" {
			t.Errorf("expected empty codec, got %q", opts.codec)
		}
		if opts.metadata != nil {
			t.Error("expected nil metadata")
		}
	})
}
