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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const placeholderPath = "/services/T0000/B0000/placeholder"

func TestSlackChannel_PostsJSONText(t *testing.T) {
	n := validNotification()
	got := make(chan string, 1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		if r.URL.Path != placeholderPath {
			t.Errorf("path %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content type %q", ct)
		}
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		got <- body.Text
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	ch, err := NewSlackChannel(SlackConfig{TenantID: "tenant-1", WebhookURL: srv.URL + placeholderPath, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("NewSlackChannel: %v", err)
	}
	if err := ch.Deliver(context.Background(), n); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if text := <-got; text != slackText(n) {
		t.Fatalf("text:\nwant %q\ngot  %q", slackText(n), text)
	}
}

func TestNewSlackChannel_RefusesNonHTTPS(t *testing.T) {
	cases := []struct {
		name  string
		cfg   SlackConfig
		field string
	}{
		{"http", SlackConfig{TenantID: "tenant-1", WebhookURL: "http://hooks.example" + placeholderPath}, "webhook_url"},
		{"empty", SlackConfig{}, "webhook_url"},
		{"unparseable", SlackConfig{TenantID: "tenant-1", WebhookURL: "::bad"}, "webhook_url"},
		{"no host", SlackConfig{TenantID: "tenant-1", WebhookURL: "https://" + placeholderPath}, "webhook_url"},
		{"no tenant", SlackConfig{WebhookURL: "https://hooks.example" + placeholderPath}, "tenant_id"},
		{"blank tenant", SlackConfig{TenantID: " ", WebhookURL: "https://hooks.example" + placeholderPath}, "tenant_id"},
		{"negative timeout", SlackConfig{TenantID: "tenant-1", WebhookURL: "https://hooks.example" + placeholderPath, Timeout: -time.Second}, "timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch, err := NewSlackChannel(tc.cfg)
			if ch != nil || !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("want ErrInvalidConfig, got %v", err)
			}
			var cfgErr *ConfigError
			if !errors.As(err, &cfgErr) || cfgErr.Field != tc.field {
				t.Fatalf("want field %q, got %v", tc.field, err)
			}
			if strings.Contains(err.Error(), "placeholder") {
				t.Fatalf("config error carries the URL: %v", err)
			}
		})
	}
}

func TestSlackChannel_StatusErrorOmitsURL(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	ch, err := NewSlackChannel(SlackConfig{TenantID: "tenant-1", WebhookURL: srv.URL + placeholderPath, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("NewSlackChannel: %v", err)
	}
	err = ch.Deliver(context.Background(), validNotification())
	var serr *SlackStatusError
	if !errors.As(err, &serr) || serr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("want *SlackStatusError 500, got %v", err)
	}
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("want ErrDeliveryFailed, got %v", err)
	}
	if strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("status error carries the URL: %v", err)
	}
}

// A Slack channel bound to one tenant never sends another tenant's
// notification to its webhook, so tenant B's subject, link and body never
// reach tenant A's Slack audience.
func TestSlackChannel_RefusesOtherTenant(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		posts.Add(1)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	ch, err := NewSlackChannel(SlackConfig{TenantID: "tenant-a", WebhookURL: srv.URL + placeholderPath, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("NewSlackChannel: %v", err)
	}
	n := validNotification()
	n.TenantID = "tenant-b"
	err = ch.Deliver(context.Background(), n)
	var terr *SlackTenantError
	if !errors.As(err, &terr) || terr.TenantID != "tenant-b" {
		t.Fatalf("want *SlackTenantError for tenant-b, got %v", err)
	}
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("want ErrDeliveryFailed, got %v", err)
	}
	if got := posts.Load(); got != 0 {
		t.Fatalf("webhook received %d posts for another tenant", got)
	}

	n.TenantID = "tenant-a"
	if err := ch.Deliver(context.Background(), n); err != nil {
		t.Fatalf("Deliver for the bound tenant: %v", err)
	}
	if got := posts.Load(); got != 1 {
		t.Fatalf("webhook received %d posts for the bound tenant, want 1", got)
	}
}

// Through a Dispatcher, a cross-tenant notification reaches no channel.
func TestDispatcher_SlackRefusesOtherTenant(t *testing.T) {
	var posts atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		posts.Add(1)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	ch, err := NewSlackChannel(SlackConfig{TenantID: "tenant-a", WebhookURL: srv.URL + placeholderPath, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("NewSlackChannel: %v", err)
	}
	d, err := NewDispatcher(DispatcherConfig{Channels: []Channel{ch}})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	n := validNotification()
	n.TenantID = "tenant-b"
	report, err := d.Notify(context.Background(), n)
	var terr *SlackTenantError
	if !errors.As(err, &terr) {
		t.Fatalf("want *SlackTenantError, got %v", err)
	}
	if len(report.Delivered) != 0 {
		t.Fatalf("delivered to %v for another tenant", report.Delivered)
	}
	if got := posts.Load(); got != 0 {
		t.Fatalf("webhook received %d posts for another tenant", got)
	}
}

func TestSlackChannel_TransportErrorOmitsURL(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	webhook := srv.URL + placeholderPath
	client := srv.Client()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	srv.Close()

	ch, err := NewSlackChannel(SlackConfig{TenantID: "tenant-1", WebhookURL: webhook, HTTPClient: client, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("NewSlackChannel: %v", err)
	}
	err = ch.Deliver(context.Background(), validNotification())
	var terr *SlackTransportError
	if !errors.As(err, &terr) {
		t.Fatalf("want *SlackTransportError, got %v", err)
	}
	if strings.Contains(err.Error(), "placeholder") || strings.Contains(err.Error(), u.Host) {
		t.Fatalf("transport error carries the URL: %v", err)
	}
}

func TestSlackChannel_IsNotSecure(t *testing.T) {
	ch, err := NewSlackChannel(SlackConfig{TenantID: "tenant-1", WebhookURL: "https://hooks.example" + placeholderPath})
	if err != nil {
		t.Fatalf("NewSlackChannel: %v", err)
	}
	if ch.Secure() {
		t.Fatalf("slack reported secure")
	}
	if ch.Name() != "slack" {
		t.Fatalf("name %q", ch.Name())
	}
}
