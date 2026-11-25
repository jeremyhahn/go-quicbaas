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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// slackChannelName is the Name of every SlackChannel.
	slackChannelName = "slack"

	// defaultSlackTimeout bounds one webhook call when none is configured.
	defaultSlackTimeout = 10 * time.Second

	// maxSlackResponseDrain bounds how much of a webhook answer is read.
	maxSlackResponseDrain = 4096
)

// SlackConfig configures a SlackChannel.
type SlackConfig struct {
	// TenantID is the tenant whose operators the webhook reaches. Required:
	// the channel delivers only that tenant's notifications.
	TenantID string

	// WebhookURL is the Slack incoming-webhook URL. It must be https. The
	// URL is the credential: it is never logged or put in an error.
	WebhookURL string

	// HTTPClient sends the request. Nil means a new http.Client.
	HTTPClient *http.Client

	// Timeout bounds one delivery. Zero means 10s; negative is refused.
	Timeout time.Duration
}

// SlackChannel delivers notifications to a Slack incoming webhook. It is
// not end-to-end encrypted, so it never receives a Sensitive body.
type SlackChannel struct {
	tenantID   string
	webhookURL string
	client     *http.Client
	timeout    time.Duration
}

// NewSlackChannel validates cfg and returns a SlackChannel. Refusals are
// *ConfigError and never include the webhook URL.
func NewSlackChannel(cfg SlackConfig) (*SlackChannel, error) {
	u, err := url.Parse(cfg.WebhookURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, &ConfigError{Field: "webhook_url", Message: "must be an https URL with a host"}
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return nil, &ConfigError{Field: "tenant_id", Message: "is required"}
	}
	if cfg.Timeout < 0 {
		return nil, &ConfigError{Field: "timeout", Message: "must not be negative"}
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultSlackTimeout
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &SlackChannel{tenantID: cfg.TenantID, webhookURL: cfg.WebhookURL, client: client, timeout: timeout}, nil
}

// Name returns "slack".
func (c *SlackChannel) Name() string { return slackChannelName }

// Secure returns false: Slack is not end-to-end encrypted.
func (c *SlackChannel) Secure() bool { return false }

// Deliver posts n to the webhook as mrkdwn text. A notification for any
// tenant other than the channel's is refused with a *SlackTenantError
// before anything is sent. A transport failure is a
// *SlackTransportError and a non-2xx answer a *SlackStatusError; neither
// contains the webhook URL. Deliver does not retry.
func (c *SlackChannel) Deliver(ctx context.Context, n Notification) error {
	if n.TenantID != c.tenantID {
		return &SlackTenantError{TenantID: n.TenantID}
	}
	payload, err := json.Marshal(struct {
		Text string `json:"text"`
	}{Text: slackText(n)})
	if err != nil {
		return &SlackTransportError{Err: err}
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.webhookURL, bytes.NewReader(payload))
	if err != nil {
		return &SlackTransportError{Err: stripURL(err)}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return &SlackTransportError{Err: stripURL(err)}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.CopyN(io.Discard, resp.Body, maxSlackResponseDrain)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &SlackStatusError{StatusCode: resp.StatusCode}
	}
	return nil
}

// errURLRedacted stands in for an error whose only content was the URL.
var errURLRedacted = errors.New("request failed")

// stripURL returns the cause inside a *url.Error, dropping the URL the
// wrapper would print. A dial error's address is dropped as well, keeping
// only the operation and its cause.
func stripURL(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		err = uerr.Err
		if err == nil {
			return errURLRedacted
		}
	}
	var operr *net.OpError
	if errors.As(err, &operr) && operr.Err != nil {
		return errors.New(operr.Op + ": " + operr.Err.Error())
	}
	var dnserr *net.DNSError
	if errors.As(err, &dnserr) {
		return errDNSFailed
	}
	return err
}

// errDNSFailed stands in for a DNS error, which names the webhook host.
var errDNSFailed = errors.New("dns lookup failed")
