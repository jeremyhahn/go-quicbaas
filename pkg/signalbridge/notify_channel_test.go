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

package signalbridge

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/notify"
)

type fakeAuthorizer struct {
	allowed map[string]bool
	err     error
}

func (f *fakeAuthorizer) MayReceive(_ context.Context, _, subjectID string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.allowed[subjectID], nil
}

// insecureFakeChannel is an insecure notify.Channel that records what it
// received.
type insecureFakeChannel struct {
	mu  sync.Mutex
	got []notify.Notification
}

func (f *insecureFakeChannel) Name() string { return "insecure" }
func (f *insecureFakeChannel) Secure() bool { return false }

func (f *insecureFakeChannel) Deliver(_ context.Context, n notify.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, n)
	return nil
}

func testNotification() notify.Notification {
	return notify.Notification{
		TenantID: "tenant-1",
		Key:      "item-1@1",
		Severity: notify.SeverityCritical,
		Subject:  "proj: task parked",
		Body:     "Question: which branch?",
		Link:     "https://forge.example/owner/repo/issues/1",
	}
}

func groupTarget() NotifyTarget {
	return NotifyTarget{ConversationID: "group-ops", Kind: ConversationGroup}
}

func TestNewNotifyChannel_Refuses(t *testing.T) {
	auth := &fakeAuthorizer{}
	cases := []struct {
		name string
		cfg  NotifyChannelConfig
	}{
		{"nil sender", NotifyChannelConfig{Targets: []NotifyTarget{groupTarget()}}},
		{"no targets", NotifyChannelConfig{Sender: &fakeSender{}}},
		{"empty conversation", NotifyChannelConfig{Sender: &fakeSender{},
			Targets: []NotifyTarget{{Kind: ConversationGroup}}}},
		{"bad kind", NotifyChannelConfig{Sender: &fakeSender{},
			Targets: []NotifyTarget{{ConversationID: "c", Kind: ConversationKind(9)}}}},
		{"dm without subject", NotifyChannelConfig{Sender: &fakeSender{}, Authorizer: auth,
			Targets: []NotifyTarget{{ConversationID: "aci-1", Kind: ConversationDM}}}},
		{"dm without authorizer", NotifyChannelConfig{Sender: &fakeSender{},
			Targets: []NotifyTarget{groupTarget(), {ConversationID: "aci-1", Kind: ConversationDM, SubjectID: "user-1"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch, err := NewNotifyChannel(tc.cfg)
			if ch != nil || !IsKind(err, ErrKindInvalidInput) {
				t.Fatalf("want ErrKindInvalidInput, got %v", err)
			}
		})
	}
}

func TestNotifyChannel_GroupTargetReceivesText(t *testing.T) {
	sender := &fakeSender{}
	ch, err := NewNotifyChannel(NotifyChannelConfig{Sender: sender, Targets: []NotifyTarget{groupTarget()}})
	if err != nil {
		t.Fatalf("NewNotifyChannel: %v", err)
	}
	n := testNotification()
	if err := ch.Deliver(context.Background(), n); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(sender.sends) != 1 {
		t.Fatalf("want 1 send, got %d", len(sender.sends))
	}
	msg := sender.sends[0]
	if msg.Body != notify.Text(n) || msg.ConversationID != "group-ops" || msg.Kind != ConversationGroup {
		t.Fatalf("message: %+v", msg)
	}
	if msg.Timestamp != 0 {
		t.Fatalf("timestamp set by the channel: %d", msg.Timestamp)
	}
}

func TestNotifyChannel_DMNeedsAuthorizedSubject(t *testing.T) {
	sender := &fakeSender{}
	auth := &fakeAuthorizer{allowed: map[string]bool{"user-allowed": true}}
	ch, err := NewNotifyChannel(NotifyChannelConfig{
		Sender:     sender,
		Authorizer: auth,
		Targets: []NotifyTarget{
			{ConversationID: "aci-denied", Kind: ConversationDM, SubjectID: "user-denied"},
			{ConversationID: "aci-allowed", Kind: ConversationDM, SubjectID: "user-allowed"},
		},
	})
	if err != nil {
		t.Fatalf("NewNotifyChannel: %v", err)
	}
	if err := ch.Deliver(context.Background(), testNotification()); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(sender.sends) != 1 || sender.sends[0].ConversationID != "aci-allowed" {
		t.Fatalf("sends: %+v", sender.sends)
	}
}

func TestNotifyChannel_AuthorizerErrorFailsClosed(t *testing.T) {
	sender := &fakeSender{}
	errAuth := errors.New("permission store unavailable")
	ch, err := NewNotifyChannel(NotifyChannelConfig{
		Sender:     sender,
		Authorizer: &fakeAuthorizer{err: errAuth},
		Targets:    []NotifyTarget{{ConversationID: "aci-secret-1", Kind: ConversationDM, SubjectID: "user-1"}},
	})
	if err != nil {
		t.Fatalf("NewNotifyChannel: %v", err)
	}
	err = ch.Deliver(context.Background(), testNotification())
	var serr *Error
	if !errors.As(err, &serr) || serr.Kind != ErrKindUnauthorized {
		t.Fatalf("want *Error unauthorized, got %v", err)
	}
	if !errors.Is(err, errAuth) {
		t.Fatalf("authorizer error not reachable: %v", err)
	}
	if len(sender.sends) != 0 {
		t.Fatalf("sent despite authorizer error: %+v", sender.sends)
	}
	if strings.Contains(err.Error(), "aci-secret-1") {
		t.Fatalf("error carries the ACI: %v", err)
	}
	if !strings.Contains(err.Error(), "user-1") {
		t.Fatalf("error does not name the subject: %v", err)
	}
}

func TestNotifyChannel_SendFailureIsTransport(t *testing.T) {
	sender := &fakeSender{failSends: true}
	ch, err := NewNotifyChannel(NotifyChannelConfig{Sender: sender, Targets: []NotifyTarget{groupTarget()}})
	if err != nil {
		t.Fatalf("NewNotifyChannel: %v", err)
	}
	err = ch.Deliver(context.Background(), testNotification())
	var serr *Error
	if !errors.As(err, &serr) || serr.Kind != ErrKindTransport {
		t.Fatalf("want *Error transport, got %v", err)
	}
}

func TestNotifyChannel_IsSecure(t *testing.T) {
	ch, err := NewNotifyChannel(NotifyChannelConfig{Sender: &fakeSender{}, Targets: []NotifyTarget{groupTarget()}})
	if err != nil {
		t.Fatalf("NewNotifyChannel: %v", err)
	}
	if !ch.Secure() || ch.Name() != "signal" {
		t.Fatalf("secure=%v name=%q", ch.Secure(), ch.Name())
	}
}

func TestNotifyChannel_WithDispatcherCarriesSensitiveBody(t *testing.T) {
	sender := &fakeSender{}
	signal, err := NewNotifyChannel(NotifyChannelConfig{Sender: sender, Targets: []NotifyTarget{groupTarget()}})
	if err != nil {
		t.Fatalf("NewNotifyChannel: %v", err)
	}
	insecure := &insecureFakeChannel{}
	d, err := notify.NewDispatcher(notify.DispatcherConfig{Channels: []notify.Channel{insecure, signal}})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	n := testNotification()
	n.Sensitive = true
	if _, err := d.Notify(context.Background(), n); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(sender.sends) != 1 || !strings.Contains(sender.sends[0].Body, n.Body) {
		t.Fatalf("signal send lacks the body: %+v", sender.sends)
	}
	if len(insecure.got) != 1 || insecure.got[0].Body != "Details were sent over signal." {
		t.Fatalf("insecure channel got: %+v", insecure.got)
	}
}
