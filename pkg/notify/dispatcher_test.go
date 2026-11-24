// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package notify

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// callLog records the order channels are called in, across channels.
type callLog struct {
	mu    sync.Mutex
	names []string
}

func (l *callLog) add(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.names = append(l.names, name)
}

type fakeChannel struct {
	name   string
	secure bool
	err    error
	log    *callLog

	mu  sync.Mutex
	got []Notification
}

func (f *fakeChannel) Name() string { return f.name }
func (f *fakeChannel) Secure() bool { return f.secure }

func (f *fakeChannel) Deliver(_ context.Context, n Notification) error {
	if f.log != nil {
		f.log.add(f.name)
	}
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, n)
	return nil
}

func (f *fakeChannel) received() []Notification {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Notification(nil), f.got...)
}

func newTestDispatcher(t *testing.T, channels ...Channel) *Dispatcher {
	t.Helper()
	d, err := NewDispatcher(DispatcherConfig{Channels: channels})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

func TestNewDispatcher_Refuses(t *testing.T) {
	cases := []struct {
		name     string
		channels []Channel
	}{
		{"no channels", nil},
		{"nil channel", []Channel{&fakeChannel{name: "signal"}, nil}},
		{"empty name", []Channel{&fakeChannel{name: ""}}},
		{"duplicate name", []Channel{&fakeChannel{name: "slack"}, &fakeChannel{name: "slack"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := NewDispatcher(DispatcherConfig{Channels: tc.channels})
			if d != nil || !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("want ErrInvalidConfig, got %v", err)
			}
			var cfgErr *ConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("want *ConfigError, got %T", err)
			}
		})
	}
}

func TestDispatcher_SecureChannelFirst(t *testing.T) {
	log := &callLog{}
	slack := &fakeChannel{name: "slack", log: log}
	signal := &fakeChannel{name: "signal", secure: true, log: log}
	d := newTestDispatcher(t, slack, signal)
	report, err := d.Notify(context.Background(), validNotification())
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(log.names) != 2 || log.names[0] != "signal" || log.names[1] != "slack" {
		t.Fatalf("call order: %v", log.names)
	}
	if len(report.Delivered) != 2 || report.Delivered[0] != "signal" {
		t.Fatalf("report: %+v", report)
	}
	if got := slack.received(); len(got) != 1 || got[0] != validNotification() {
		t.Fatalf("non-sensitive notification changed on insecure channel: %+v", got)
	}
}

func TestDispatcher_SensitiveBodyStaysOnSecureChannel(t *testing.T) {
	slack := &fakeChannel{name: "slack"}
	signal := &fakeChannel{name: "signal", secure: true}
	d := newTestDispatcher(t, slack, signal)
	n := validNotification()
	n.Sensitive = true
	if _, err := d.Notify(context.Background(), n); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	sec := signal.received()
	if len(sec) != 1 || sec[0].Body != n.Body {
		t.Fatalf("secure channel body: %+v", sec)
	}
	ins := slack.received()
	if len(ins) != 1 {
		t.Fatalf("insecure channel got %d", len(ins))
	}
	if ins[0].Body != "Details were sent over signal." {
		t.Fatalf("insecure body: %q", ins[0].Body)
	}
	if ins[0].Subject != n.Subject || ins[0].Link != n.Link || ins[0].Severity != n.Severity {
		t.Fatalf("insecure notification lost fields: %+v", ins[0])
	}
}

func TestDispatcher_SensitiveWithheldWhenSecureFails(t *testing.T) {
	slack := &fakeChannel{name: "slack"}
	signal := &fakeChannel{name: "signal", secure: true, err: errors.New("signal down")}
	d := newTestDispatcher(t, signal, slack)
	n := validNotification()
	n.Sensitive = true
	report, err := d.Notify(context.Background(), n)
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	ins := slack.received()
	if len(ins) != 1 || ins[0].Body != withheldBody {
		t.Fatalf("insecure body: %+v", ins)
	}
	if len(report.Failed) != 1 || report.Failed[0].Channel != "signal" {
		t.Fatalf("report: %+v", report)
	}
}

func TestDispatcher_PartialFailureIsDelivered(t *testing.T) {
	slack := &fakeChannel{name: "slack", err: errors.New("slack down")}
	signal := &fakeChannel{name: "signal", secure: true}
	d := newTestDispatcher(t, slack, signal)
	report, err := d.Notify(context.Background(), validNotification())
	if err != nil {
		t.Fatalf("partial success returned %v", err)
	}
	if len(report.Delivered) != 1 || report.Delivered[0] != "signal" {
		t.Fatalf("delivered: %+v", report.Delivered)
	}
	if len(report.Failed) != 1 || report.Failed[0].Channel != "slack" {
		t.Fatalf("failed: %+v", report.Failed)
	}
}

func TestDispatcher_NoChannelDeliveredReturnsDeliveryError(t *testing.T) {
	errSignal := errors.New("signal down")
	slack := &fakeChannel{name: "slack", err: errors.New("slack down")}
	signal := &fakeChannel{name: "signal", secure: true, err: errSignal}
	d := newTestDispatcher(t, slack, signal)
	report, err := d.Notify(context.Background(), validNotification())
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("want ErrDeliveryFailed, got %v", err)
	}
	var derr *DeliveryError
	if !errors.As(err, &derr) || derr.Key != "item-1@1" || len(derr.Failures) != 2 {
		t.Fatalf("want *DeliveryError with two failures, got %v", err)
	}
	if !errors.Is(err, errSignal) {
		t.Fatalf("channel error not reachable through %v", err)
	}
	if len(report.Delivered) != 0 {
		t.Fatalf("report: %+v", report)
	}
}

func TestDispatcher_InvalidNotificationDeliversNothing(t *testing.T) {
	slack := &fakeChannel{name: "slack"}
	signal := &fakeChannel{name: "signal", secure: true}
	d := newTestDispatcher(t, slack, signal)
	n := validNotification()
	n.Subject = ""
	if _, err := d.Notify(context.Background(), n); !errors.Is(err, ErrInvalidNotification) {
		t.Fatalf("want ErrInvalidNotification, got %v", err)
	}
	if len(slack.received()) != 0 || len(signal.received()) != 0 {
		t.Fatalf("invalid notification was delivered")
	}
}

func TestDispatcher_NotifyChannelsOnlyNamed(t *testing.T) {
	log := &callLog{}
	signal := &fakeChannel{name: "signal-like", secure: true, log: log}
	slack := &fakeChannel{name: "slack-like", log: log}
	d := newTestDispatcher(t, slack, signal)
	report, err := d.NotifyChannels(context.Background(), validNotification(), []string{"slack-like"})
	if err != nil {
		t.Fatalf("NotifyChannels: %v", err)
	}
	if len(report.Delivered) != 1 || report.Delivered[0] != "slack-like" || len(report.Failed) != 0 {
		t.Fatalf("report: %+v", report)
	}
	if len(slack.received()) != 1 || len(signal.received()) != 0 {
		t.Fatalf("slack %d, signal %d", len(slack.received()), len(signal.received()))
	}
	if len(log.names) != 1 || log.names[0] != "slack-like" {
		t.Fatalf("calls: %v", log.names)
	}
}

func TestDispatcher_NotifyChannelsRefusesUnknownName(t *testing.T) {
	log := &callLog{}
	signal := &fakeChannel{name: "signal-like", secure: true, log: log}
	slack := &fakeChannel{name: "slack-like", log: log}
	d := newTestDispatcher(t, slack, signal)
	_, err := d.NotifyChannels(context.Background(), validNotification(), []string{"signal-like", "pager"})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) || cfgErr.Field != "channels" {
		t.Fatalf("want *ConfigError on channels, got %v", err)
	}
	if len(log.names) != 0 || len(slack.received()) != 0 || len(signal.received()) != 0 {
		t.Fatalf("a refused call delivered: %v", log.names)
	}
}
