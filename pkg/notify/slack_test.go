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

	ch, err := NewSlackChannel(SlackConfig{WebhookURL: srv.URL + placeholderPath, HTTPClient: srv.Client()})
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
		{"http", SlackConfig{WebhookURL: "http://hooks.example" + placeholderPath}, "webhook_url"},
		{"empty", SlackConfig{}, "webhook_url"},
		{"unparseable", SlackConfig{WebhookURL: "::bad"}, "webhook_url"},
		{"no host", SlackConfig{WebhookURL: "https://" + placeholderPath}, "webhook_url"},
		{"negative timeout", SlackConfig{WebhookURL: "https://hooks.example" + placeholderPath, Timeout: -time.Second}, "timeout"},
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
	ch, err := NewSlackChannel(SlackConfig{WebhookURL: srv.URL + placeholderPath, HTTPClient: srv.Client()})
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

func TestSlackChannel_TransportErrorOmitsURL(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	webhook := srv.URL + placeholderPath
	client := srv.Client()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	srv.Close()

	ch, err := NewSlackChannel(SlackConfig{WebhookURL: webhook, HTTPClient: client, Timeout: 5 * time.Second})
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
	ch, err := NewSlackChannel(SlackConfig{WebhookURL: "https://hooks.example" + placeholderPath})
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
