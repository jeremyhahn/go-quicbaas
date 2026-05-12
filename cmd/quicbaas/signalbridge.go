// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

// Package main provides CLI subcommands that drive the Signal
// orchestrator bot (pkg/signalbridge). Three verbs are exposed:
//
//	signalbridge serve     run the bot against a YAML config
//	signalbridge register  register a fresh account on the go-signal server
//	signalbridge status    print the bot's persisted state summary
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/signalbridge"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

var (
	sbConfigPath    string
	sbBotIdentity   string
	sbGoSignalURL   string
	sbDeviceName    string
	sbPasswordSeed  string
	sbStatePath     string
)

// newSignalBridgeCommand builds the `quicbaas signalbridge` subtree.
func newSignalBridgeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "signalbridge",
		Short: "Signal-messenger chat bridge for the QuicBaaS orchestrator",
		Long: `Register a Signal account, run the orchestrator bot, or inspect its state.

The bot registers as a normal Signal client on a user-deployed go-signal
server and exposes the orchestrator over DMs + group chats.`,
	}
	cmd.AddCommand(newSignalBridgeServeCmd())
	cmd.AddCommand(newSignalBridgeRegisterCmd())
	cmd.AddCommand(newSignalBridgeStatusCmd())
	return cmd
}

// newSignalBridgeServeCmd wires the long-running bot process.
func newSignalBridgeServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the Signal orchestrator bot",
		RunE:  runSignalBridgeServe,
	}
	cmd.Flags().StringVar(&sbConfigPath, "config", "/etc/quicbaas/signalbridge.yaml", "Bot config YAML path")
	return cmd
}

// newSignalBridgeRegisterCmd drives a one-shot interactive registration.
func newSignalBridgeRegisterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register a new Signal account for the bot",
		RunE:  runSignalBridgeRegister,
	}
	cmd.Flags().StringVar(&sbBotIdentity, "bot-identity", "", "E.164 phone number or username for the bot")
	cmd.Flags().StringVar(&sbGoSignalURL, "go-signal-url", "", "Base URL of the go-signal server")
	cmd.Flags().StringVar(&sbDeviceName, "device-name", "orchestrator-bot", "Device name advertised to peers")
	cmd.Flags().StringVar(&sbConfigPath, "config", "", "Config YAML path (required for --state-path default)")
	cmd.Flags().StringVar(&sbStatePath, "state-path", "", "Override state directory")
	_ = cmd.MarkFlagRequired("bot-identity")
	_ = cmd.MarkFlagRequired("go-signal-url")
	return cmd
}

// newSignalBridgeStatusCmd prints a one-page summary of bot state.
func newSignalBridgeStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print the bot's persisted state summary",
		RunE:  runSignalBridgeStatus,
	}
	cmd.Flags().StringVar(&sbConfigPath, "config", "/etc/quicbaas/signalbridge.yaml", "Bot config YAML path")
	return cmd
}

// runSignalBridgeServe is the entry point for `signalbridge serve`.
// Heavy lifting — wiring SignalClient + HTTPTransport + SDK client +
// AuthBridge + SessionManager + EventTranslator — lives here so the
// package remains a pure library.
func runSignalBridgeServe(cmd *cobra.Command, args []string) error {
	cfg, err := signalbridge.LoadBotConfig(sbConfigPath)
	if err != nil {
		return err
	}

	statePath := filepath.Join(cfg.StatePath, "state.json")
	store, err := signalbridge.NewFileStateStore(statePath, true)
	if err != nil {
		return err
	}
	defer store.Close()

	account, err := store.LoadAccount()
	if err != nil {
		if !signalbridge.IsKind(err, signalbridge.ErrKindNotFound) {
			return err
		}
		return errors.New("no registered account; run `quicbaas signalbridge register` first")
	}

	tlsCfg, err := buildSignalBridgeTLSConfig(cfg)
	if err != nil {
		return err
	}
	httpT, err := signalbridge.NewHTTPTransport(signalbridge.HTTPTransportConfig{
		BaseURL:         cfg.GoSignalURL,
		TLSClientConfig: tlsCfg,
		Logger: asInterfaceLogger(log),
	})
	if err != nil {
		return err
	}
	adapter := signalbridge.NewLibSignalAdapter()
	client, err := signalbridge.NewSignalClient(signalbridge.SignalClientConfig{
		ServerURL:               cfg.GoSignalURL,
		Account:                 *account,
		Transport:               httpT,
		Protocol:                adapter,
		Logger: asInterfaceLogger(log),
		RateLimit:               cfg.RateLimit,
		PreKeyRotationThreshold: cfg.PreKeyRotationThreshold,
		PreKeyCheckInterval:     cfg.PreKeyCheckInterval,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	if err := client.Login(ctx, signalbridge.LoginOptions{Account: *account, Subscribe: true}); err != nil {
		return err
	}
	defer client.Close()

	// Build orchestrator SDK client.
	sdkClient, err := quicbaas.NewClient(cfg.QuicBaaSAddr)
	if err != nil {
		return err
	}
	defer sdkClient.Close()

	manager, err := signalbridge.NewSessionManager(store, cfg.VerbosityDefaults)
	if err != nil {
		return err
	}
	translator, err := signalbridge.NewEventTranslator(client, manager)
	if err != nil {
		return err
	}

	auth, err := signalbridge.NewAuthBridge(signalbridge.AuthBridgeConfig{
		Mode:           cfg.AuthMode,
		TenantID:       cfg.TenantID,
		ServiceAccount: cfg.ServiceAccount,
		Links:          cfg.LinkedOperators,
		AllowedGroups:  convertAllowedGroups(cfg.AllowedGroups),
	})
	if err != nil {
		return err
	}

	bot, err := signalbridge.NewOrchestratorBot(signalbridge.BotConfigRuntime{
		Config:       cfg,
		Signal:       client,
		Orchestrator: sdkClient.Orchestrator(),
		Auth:         auth,
		Manager:      manager,
		Translator:   translator,
		Parser:       signalbridge.NewIntentParser(),
		Logger: asInterfaceLogger(log),
	})
	if err != nil {
		return err
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			_ = bot.Stop()
		case <-ctx.Done():
		}
	}()

	log.Info("signalbridge: starting bot",
		"identity", cfg.BotIdentity,
		"tenant", cfg.TenantID,
		"auth_mode", string(cfg.AuthMode),
	)
	return bot.Run(ctx)
}

// runSignalBridgeRegister is the entry point for `signalbridge register`.
func runSignalBridgeRegister(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	statePath := sbStatePath
	if statePath == "" && sbConfigPath != "" {
		cfg, err := signalbridge.LoadBotConfig(sbConfigPath)
		if err == nil {
			statePath = cfg.StatePath
		}
	}
	if statePath == "" {
		return errors.New("--state-path required when no --config")
	}
	store, err := signalbridge.NewFileStateStore(filepath.Join(statePath, "state.json"), true)
	if err != nil {
		return err
	}
	defer store.Close()

	httpT, err := signalbridge.NewHTTPTransport(signalbridge.HTTPTransportConfig{
		BaseURL: sbGoSignalURL,
		Logger:  asInterfaceLogger(log),
	})
	if err != nil {
		return err
	}
	adapter := signalbridge.NewLibSignalAdapter()
	client, err := signalbridge.NewSignalClient(signalbridge.SignalClientConfig{
		ServerURL: sbGoSignalURL,
		Transport: httpT,
		Protocol:  adapter,
		Logger: asInterfaceLogger(log),
	})
	if err != nil {
		return err
	}

	// Phase 1 — create session, request code.
	_, err = client.Register(ctx, signalbridge.RegisterOptions{
		Number:     sbBotIdentity,
		DeviceName: sbDeviceName,
	})
	if err == nil {
		return errors.New("unexpected success without verification code")
	}
	bridgeErr, ok := err.(*signalbridge.Error)
	if !ok || bridgeErr.Kind != signalbridge.ErrKindVerificationRequired {
		return err
	}
	sessionID := bridgeErr.Msg

	// Phase 2 — collect code from stdin.
	fmt.Fprint(os.Stdout, "Enter verification code: ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	code := strings.TrimSpace(line)
	if code == "" {
		return errors.New("empty verification code")
	}

	// Phase 3 — complete registration.
	account, err := client.Register(ctx, signalbridge.RegisterOptions{
		Number:           sbBotIdentity,
		DeviceName:       sbDeviceName,
		SessionID:        sessionID,
		VerificationCode: code,
	})
	if err != nil {
		return err
	}
	if err := store.SaveAccount(account); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "registered: aci=%s pni=%s device=%d\n", account.ACI, account.PNI, account.DeviceID)
	return nil
}

// runSignalBridgeStatus is the entry point for `signalbridge status`.
func runSignalBridgeStatus(cmd *cobra.Command, args []string) error {
	cfg, err := signalbridge.LoadBotConfig(sbConfigPath)
	if err != nil {
		return err
	}
	statePath := filepath.Join(cfg.StatePath, "state.json")
	store, err := signalbridge.NewFileStateStore(statePath, false)
	if err != nil {
		return err
	}
	defer store.Close()

	account, err := store.LoadAccount()
	if err != nil {
		fmt.Fprintf(os.Stdout, "account: (not registered)\n")
	} else {
		fmt.Fprintf(os.Stdout, "account: aci=%s device=%d number=%s\n", account.ACI, account.DeviceID, account.Number)
	}
	convs, err := store.ListConversations()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "conversations: %d\n", len(convs))
	for _, c := range convs {
		fmt.Fprintf(os.Stdout, "  %s: attached=%q watermark=%d updated=%s\n",
			c.ConversationID, c.AttachedSessionID, c.LastDeliveredSeq, c.UpdatedAt.Format(time.RFC3339))
	}
	return nil
}

// buildTLSConfig assembles a tls.Config from the bot config's CA/cert
// paths. Returns nil when no TLS customization is requested.
func buildSignalBridgeTLSConfig(cfg *signalbridge.BotConfig) (*tls.Config, error) {
	if cfg.QuicBaaSCA == "" && cfg.QuicBaaSCert == "" {
		return nil, nil
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if cfg.QuicBaaSCA != "" {
		caBytes, err := os.ReadFile(cfg.QuicBaaSCA)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(caBytes)
		tlsCfg.RootCAs = pool
	}
	if cfg.QuicBaaSCert != "" && cfg.QuicBaaSKey != "" {
		cert, err := tls.LoadX509KeyPair(cfg.QuicBaaSCert, cfg.QuicBaaSKey)
		if err != nil {
			return nil, err
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	return tlsCfg, nil
}

// convertAllowedGroups converts the slice representation in YAML to
// the set-style map the AuthBridge expects.
func convertAllowedGroups(list []string) map[string]struct{} {
	m := make(map[string]struct{}, len(list))
	for _, id := range list {
		m[id] = struct{}{}
	}
	return m
}
