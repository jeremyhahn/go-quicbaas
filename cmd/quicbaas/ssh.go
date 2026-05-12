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
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
)

// =============================================================================
// `quicbaas ssh` — SSH key, known-hosts, authorized-keys management
// =============================================================================
//
// Transport is selected by the environment: XKMSD_URL → REST, empty →
// file-backed embedded store under $HOME/.quicbaas/ssh-store. Both
// paths go through buildCLISSHClient so the CLI and the agent deploy
// path pick the same store.

// =============================================================================
// Typed errors
// =============================================================================

var (
	// ErrSSHMissingKeyName is returned when a required --name flag is
	// absent.
	ErrSSHMissingKeyName = errors.New("ssh: --name is required")

	// ErrSSHMissingHost is returned when a host argument is absent.
	ErrSSHMissingHost = errors.New("ssh: host is required")

	// ErrSSHMissingFingerprint is returned when a fingerprint is absent.
	ErrSSHMissingFingerprint = errors.New("ssh: fingerprint is required")

	// ErrSSHInvalidKeyType is returned when --type is outside the
	// supported algorithm set.
	ErrSSHInvalidKeyType = errors.New("ssh: invalid key type")
)

// SSHReadPEMError wraps a filesystem failure reading a PEM file
// passed to `quicbaas ssh keys import --from-file`. Kept as a typed
// error so scripts can distinguish "no file" from "invalid PEM".
type SSHReadPEMError struct {
	Path string
	Err  error
}

// Error implements the error interface.
func (e *SSHReadPEMError) Error() string {
	return "ssh: read pem " + e.Path + ": " + e.Err.Error()
}

// Unwrap returns the underlying filesystem error.
func (e *SSHReadPEMError) Unwrap() error { return e.Err }

// =============================================================================
// Root command tree
// =============================================================================

// newSSHCommand builds the `quicbaas ssh` subcommand tree. Registration
// on the root command happens in main.go via rootCmd.AddCommand.
func newSSHCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ssh",
		Short: "SSH key, known-hosts, and authorized-keys management",
		Long: `SSH key, host-key, and authorized-key management via the xkms SDK.

Transport selection:
  * When XKMSD_URL is set the CLI uses the REST transport to talk to
    xkmsd. Optional bearer token: XKMSD_TOKEN.
  * Otherwise the CLI uses the embedded transport with a file-backed
    store at $HOME/.quicbaas/ssh-store.

Both transports expose the same operations, so scripts written
against one work unchanged against the other.`,
	}

	cmd.AddCommand(newSSHKeysCommand())
	cmd.AddCommand(newSSHKnownHostsCommand())
	cmd.AddCommand(newSSHAuthorizedCommand())
	return cmd
}

// =============================================================================
// keys
// =============================================================================

// newSSHKeysCommand builds the `quicbaas ssh keys` subtree.
func newSSHKeysCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "SSH key management",
	}
	cmd.AddCommand(newSSHKeysGenerateCmd())
	cmd.AddCommand(newSSHKeysImportCmd())
	cmd.AddCommand(newSSHKeysListCmd())
	cmd.AddCommand(newSSHKeysGetCmd())
	cmd.AddCommand(newSSHKeysDeleteCmd())
	cmd.AddCommand(newSSHKeysExportCmd())
	return cmd
}

func newSSHKeysGenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a new SSH key",
		RunE:  runSSHKeysGenerate,
	}
	addKeysSelectorFlags(cmd)
	cmd.Flags().String("type", "ed25519", "Key algorithm (ed25519, rsa, ecdsa-p256, ecdsa-p384, ecdsa-p521)")
	cmd.Flags().Int("bits", 0, "RSA key size (default 4096 for rsa)")
	cmd.Flags().String("comment", "", "Free-form key comment")
	return cmd
}

func newSSHKeysImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import an existing PEM-encoded SSH private key",
		RunE:  runSSHKeysImport,
	}
	addKeysSelectorFlags(cmd)
	cmd.Flags().String("from-file", "", "Path to the PEM file to import (required)")
	cmd.Flags().String("comment", "", "Free-form key comment")
	return cmd
}

func newSSHKeysListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List SSH keys",
		RunE:    runSSHKeysList,
	}
	addKeysListFlags(cmd)
	return cmd
}

func newSSHKeysGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Show metadata for a single SSH key",
		RunE:  runSSHKeysGet,
	}
	addKeysSelectorFlags(cmd)
	return cmd
}

func newSSHKeysDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete",
		Aliases: []string{"rm", "remove"},
		Short:   "Delete an SSH key",
		RunE:    runSSHKeysDelete,
	}
	addKeysSelectorFlags(cmd)
	return cmd
}

func newSSHKeysExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Print the authorized_keys-format public half",
		RunE:  runSSHKeysExport,
	}
	addKeysSelectorFlags(cmd)
	return cmd
}

// =============================================================================
// known-hosts
// =============================================================================

// newSSHKnownHostsCommand builds the `quicbaas ssh known-hosts` subtree.
func newSSHKnownHostsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "known-hosts",
		Aliases: []string{"host-keys"},
		Short:   "SSH host-key pin management",
	}
	cmd.AddCommand(newSSHKnownHostsAddCmd())
	cmd.AddCommand(newSSHKnownHostsListCmd())
	cmd.AddCommand(newSSHKnownHostsRemoveCmd())
	return cmd
}

func newSSHKnownHostsAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add (pin) a host-key fingerprint",
		RunE:  runSSHKnownHostsAdd,
	}
	addKnownHostsSelectorFlags(cmd)
	cmd.Flags().String("host", "", "Target host (required)")
	cmd.Flags().String("fingerprint", "", "SSH SHA256 fingerprint (required)")
	cmd.Flags().String("algorithm", "", "Host-key algorithm (e.g. ssh-ed25519)")
	cmd.Flags().String("source", "", "Provenance tag (manual, cert, ...)")
	return cmd
}

func newSSHKnownHostsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List pinned host-key fingerprints",
		RunE:    runSSHKnownHostsList,
	}
	addKnownHostsSelectorFlags(cmd)
	return cmd
}

func newSSHKnownHostsRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a pinned host-key fingerprint",
		RunE:    runSSHKnownHostsRemove,
	}
	addKnownHostsSelectorFlags(cmd)
	cmd.Flags().String("host", "", "Target host (required)")
	return cmd
}

// =============================================================================
// authorized-keys
// =============================================================================

// newSSHAuthorizedCommand builds the `quicbaas ssh authorized` subtree.
func newSSHAuthorizedCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "authorized",
		Aliases: []string{"authorized-keys"},
		Short:   "Authorized-key management",
	}
	cmd.AddCommand(newSSHAuthorizedAddCmd())
	cmd.AddCommand(newSSHAuthorizedListCmd())
	cmd.AddCommand(newSSHAuthorizedRemoveCmd())
	return cmd
}

func newSSHAuthorizedAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [line]",
		Short: "Add an authorized_keys line",
		Args:  cobra.ExactArgs(1),
		RunE:  runSSHAuthorizedAdd,
	}
	addAuthorizedSelectorFlags(cmd)
	return cmd
}

func newSSHAuthorizedListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List authorized_keys entries",
		RunE:    runSSHAuthorizedList,
	}
	addAuthorizedSelectorFlags(cmd)
	return cmd
}

func newSSHAuthorizedRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove an authorized_keys entry",
		RunE:    runSSHAuthorizedRemove,
	}
	addAuthorizedSelectorFlags(cmd)
	return cmd
}

// =============================================================================
// Shared flag helpers
// =============================================================================

// addKeysSelectorFlags adds the (scope, owner, name) trio used by most
// keys subcommands.
func addKeysSelectorFlags(cmd *cobra.Command) {
	cmd.Flags().String("scope", "", "Scope (tenant|user|service) — defaults to user")
	cmd.Flags().String("owner", "", "Owner identifier — defaults to $USER")
	cmd.Flags().String("name", "", "Key name (required for most operations)")
}

// addKeysListFlags adds just scope/owner for list operations that
// don't take a name.
func addKeysListFlags(cmd *cobra.Command) {
	cmd.Flags().String("scope", "", "Scope — defaults to user")
	cmd.Flags().String("owner", "", "Owner identifier — defaults to $USER")
}

// addKnownHostsSelectorFlags adds scope/owner for host-key subcommands.
func addKnownHostsSelectorFlags(cmd *cobra.Command) {
	cmd.Flags().String("scope", "", "Scope — defaults to user")
	cmd.Flags().String("owner", "", "Owner identifier — defaults to $USER")
}

// addAuthorizedSelectorFlags adds scope/owner/name for authorized_keys
// subcommands.
func addAuthorizedSelectorFlags(cmd *cobra.Command) {
	cmd.Flags().String("scope", "", "Scope — defaults to user")
	cmd.Flags().String("owner", "", "Owner identifier — defaults to $USER")
	cmd.Flags().String("name", "", "Entry name")
}

// =============================================================================
// Handlers — keys
// =============================================================================

func runSSHKeysGenerate(cmd *cobra.Command, _ []string) error {
	scope, owner, name, err := readKeySelector(cmd)
	if err != nil {
		return err
	}
	algo, _ := cmd.Flags().GetString("type")
	bits, _ := cmd.Flags().GetInt("bits")
	comment, _ := cmd.Flags().GetString("comment")
	if algo == "" {
		algo = "ed25519"
	}
	if !isValidSSHAlgorithm(algo) {
		return fmt.Errorf("%w: %q", ErrSSHInvalidKeyType, algo)
	}

	client, closer, err := sshClientWithCloser()
	if err != nil {
		return err
	}
	defer closer()

	out, err := client.Keys().Generate(context.Background(), xkmsssh.GenerateRequest{
		Scope: scope, Owner: owner, Name: name,
		Algorithm: algo, RSABits: bits, Comment: comment,
	})
	if err != nil {
		return err
	}
	printSSHKey(cmd, out)
	return nil
}

func runSSHKeysImport(cmd *cobra.Command, _ []string) error {
	scope, owner, name, err := readKeySelectorAllowMissingName(cmd)
	if err != nil {
		return err
	}
	fromFile, _ := cmd.Flags().GetString("from-file")
	if strings.TrimSpace(fromFile) == "" {
		return &SSHReadPEMError{Path: "", Err: errors.New("--from-file is required")}
	}
	comment, _ := cmd.Flags().GetString("comment")
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(fromFile), filepath.Ext(fromFile))
	}

	pemBytes, err := os.ReadFile(fromFile)
	if err != nil {
		return &SSHReadPEMError{Path: fromFile, Err: err}
	}

	client, closer, err := sshClientWithCloser()
	if err != nil {
		return err
	}
	defer closer()

	provider := sshPassphraseProvider()
	out, err := client.Keys().Import(context.Background(), xkmsssh.ImportRequest{
		Scope: scope, Owner: owner, Name: name, Comment: comment, PEM: pemBytes,
	}, provider)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Imported SSH key: %s/%s/%s\n", out.Scope, out.Owner, out.Name)
	fmt.Fprintf(cmd.OutOrStdout(), "  Fingerprint: %s\n", out.Fingerprint)
	return nil
}

func runSSHKeysList(cmd *cobra.Command, _ []string) error {
	scope, _ := cmd.Flags().GetString("scope")
	owner, _ := cmd.Flags().GetString("owner")

	client, closer, err := sshClientWithCloser()
	if err != nil {
		return err
	}
	defer closer()

	list, err := client.Keys().List(context.Background(), xkmsssh.ListKeysRequest{
		Scope: scope, Owner: owner,
	})
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No SSH keys found.")
		return nil
	}
	for _, k := range list {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s/%s/%s  %s  %s\n",
			k.Scope, k.Owner, k.Name, k.Algorithm, k.Fingerprint)
	}
	return nil
}

func runSSHKeysGet(cmd *cobra.Command, _ []string) error {
	scope, owner, name, err := readKeySelector(cmd)
	if err != nil {
		return err
	}
	client, closer, err := sshClientWithCloser()
	if err != nil {
		return err
	}
	defer closer()
	out, err := client.Keys().Get(context.Background(), xkmsssh.GetKeyRequest{
		Scope: scope, Owner: owner, Name: name,
	})
	if err != nil {
		return err
	}
	printSSHKey(cmd, out)
	return nil
}

func runSSHKeysDelete(cmd *cobra.Command, _ []string) error {
	scope, owner, name, err := readKeySelector(cmd)
	if err != nil {
		return err
	}
	client, closer, err := sshClientWithCloser()
	if err != nil {
		return err
	}
	defer closer()
	if err := client.Keys().Delete(context.Background(), xkmsssh.DeleteKeyRequest{
		Scope: scope, Owner: owner, Name: name,
	}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Deleted %s/%s/%s\n", scope, owner, name)
	return nil
}

func runSSHKeysExport(cmd *cobra.Command, _ []string) error {
	scope, owner, name, err := readKeySelector(cmd)
	if err != nil {
		return err
	}
	client, closer, err := sshClientWithCloser()
	if err != nil {
		return err
	}
	defer closer()
	out, err := client.Keys().ExportPublic(context.Background(), xkmsssh.ExportRequest{
		Scope: scope, Owner: owner, Name: name,
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), out.Line)
	return nil
}

// =============================================================================
// Handlers — known-hosts
// =============================================================================

func runSSHKnownHostsAdd(cmd *cobra.Command, _ []string) error {
	scope, owner := defaultScopeAndOwner(cmd)
	host, _ := cmd.Flags().GetString("host")
	fp, _ := cmd.Flags().GetString("fingerprint")
	algo, _ := cmd.Flags().GetString("algorithm")
	src, _ := cmd.Flags().GetString("source")
	if host == "" {
		return ErrSSHMissingHost
	}
	if fp == "" {
		return ErrSSHMissingFingerprint
	}
	client, closer, err := sshClientWithCloser()
	if err != nil {
		return err
	}
	defer closer()
	if err := client.KnownHosts().Add(context.Background(), xkmsssh.AddKnownHostRequest{
		Scope: scope, Owner: owner, Host: host,
		Fingerprint: fp, Algorithm: algo, Source: src,
	}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Pinned %s -> %s\n", host, fp)
	return nil
}

func runSSHKnownHostsList(cmd *cobra.Command, _ []string) error {
	scope, owner := defaultScopeAndOwner(cmd)
	client, closer, err := sshClientWithCloser()
	if err != nil {
		return err
	}
	defer closer()
	list, err := client.KnownHosts().List(context.Background(), xkmsssh.ListKnownHostsRequest{
		Scope: scope, Owner: owner,
	})
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No host-key pins found.")
		return nil
	}
	for _, p := range list {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s/%s  %s  %s  %s\n",
			p.Scope, p.Owner, p.Host, p.Fingerprint, p.Algorithm)
	}
	return nil
}

func runSSHKnownHostsRemove(cmd *cobra.Command, _ []string) error {
	scope, owner := defaultScopeAndOwner(cmd)
	host, _ := cmd.Flags().GetString("host")
	if host == "" {
		return ErrSSHMissingHost
	}
	client, closer, err := sshClientWithCloser()
	if err != nil {
		return err
	}
	defer closer()
	if err := client.KnownHosts().Remove(context.Background(), xkmsssh.RemoveKnownHostRequest{
		Scope: scope, Owner: owner, Host: host,
	}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed pin for %s\n", host)
	return nil
}

// =============================================================================
// Handlers — authorized
// =============================================================================

func runSSHAuthorizedAdd(cmd *cobra.Command, args []string) error {
	scope, owner := defaultScopeAndOwner(cmd)
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return ErrSSHMissingKeyName
	}
	client, closer, err := sshClientWithCloser()
	if err != nil {
		return err
	}
	defer closer()
	if err := client.AuthorizedKeys().Add(context.Background(), xkmsssh.AddAuthorizedKeyRequest{
		Scope: scope, Owner: owner, Name: name, Line: args[0],
	}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Added authorized entry %s/%s/%s\n", scope, owner, name)
	return nil
}

func runSSHAuthorizedList(cmd *cobra.Command, _ []string) error {
	scope, owner := defaultScopeAndOwner(cmd)
	client, closer, err := sshClientWithCloser()
	if err != nil {
		return err
	}
	defer closer()
	list, err := client.AuthorizedKeys().List(context.Background(), xkmsssh.ListAuthorizedKeysRequest{
		Scope: scope, Owner: owner,
	})
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No authorized entries.")
		return nil
	}
	for _, e := range list {
		fmt.Fprintln(cmd.OutOrStdout(), e.Line)
	}
	return nil
}

func runSSHAuthorizedRemove(cmd *cobra.Command, _ []string) error {
	scope, owner := defaultScopeAndOwner(cmd)
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return ErrSSHMissingKeyName
	}
	client, closer, err := sshClientWithCloser()
	if err != nil {
		return err
	}
	defer closer()
	if err := client.AuthorizedKeys().Remove(context.Background(), scope, owner, name); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed authorized entry %s/%s/%s\n", scope, owner, name)
	return nil
}

// =============================================================================
// Helpers
// =============================================================================

// sshClientWithCloser builds an xkms SSH SDK client and returns a
// closer function suitable for use with defer. The closer is always
// safe to call — a no-op on the REST transport.
func sshClientWithCloser() (xkmsssh.Client, func(), error) {
	client, err := buildCLISSHClient()
	if err != nil {
		return nil, nil, err
	}
	return client, func() { _ = client.Close() }, nil
}

// readKeySelector reads scope/owner/name with defaults applied and
// returns an error when name is empty (most commands require it).
func readKeySelector(cmd *cobra.Command) (string, string, string, error) {
	scope, _ := cmd.Flags().GetString("scope")
	owner, _ := cmd.Flags().GetString("owner")
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return "", "", "", ErrSSHMissingKeyName
	}
	if scope == "" {
		scope = "user"
	}
	if owner == "" {
		owner = cliSSHOwner()
	}
	return scope, owner, name, nil
}

// readKeySelectorAllowMissingName is used by import where the name
// defaults to the imported file's basename when absent.
func readKeySelectorAllowMissingName(cmd *cobra.Command) (string, string, string, error) {
	scope, _ := cmd.Flags().GetString("scope")
	owner, _ := cmd.Flags().GetString("owner")
	name, _ := cmd.Flags().GetString("name")
	if scope == "" {
		scope = "user"
	}
	if owner == "" {
		owner = cliSSHOwner()
	}
	return scope, owner, name, nil
}

// defaultScopeAndOwner returns scope/owner with the CLI-wide defaults
// applied. Used by known-hosts and authorized-keys commands.
func defaultScopeAndOwner(cmd *cobra.Command) (string, string) {
	scope, _ := cmd.Flags().GetString("scope")
	owner, _ := cmd.Flags().GetString("owner")
	if scope == "" {
		scope = "user"
	}
	if owner == "" {
		owner = cliSSHOwner()
	}
	return scope, owner
}

// sshPassphraseProvider picks a passphrase provider appropriate to the
// CLI's attachment: TTY → TerminalProvider (no echo), piped stdin →
// StdinProvider.
func sshPassphraseProvider() secure.PassphraseProvider {
	if isStdinTTY() {
		return secure.NewTerminalProvider()
	}
	return secure.NewStdinProvider()
}

// isStdinTTY reports whether stdin is attached to a terminal.
func isStdinTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// printSSHKey emits the standard metadata block for a *xkmsssh.Key.
func printSSHKey(cmd *cobra.Command, k *xkmsssh.Key) {
	if k == nil {
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Key: %s/%s/%s\n", k.Scope, k.Owner, k.Name)
	fmt.Fprintf(cmd.OutOrStdout(), "  Algorithm:   %s\n", k.Algorithm)
	fmt.Fprintf(cmd.OutOrStdout(), "  Fingerprint: %s\n", k.Fingerprint)
	if k.Comment != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Comment:     %s\n", k.Comment)
	}
	if k.PublicKey != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Public:      %s\n", k.PublicKey)
	}
}

// isValidSSHAlgorithm reports whether a is one of the supported SSH
// algorithm identifiers.
func isValidSSHAlgorithm(a string) bool {
	switch a {
	case "ed25519", "rsa", "ecdsa-p256", "ecdsa-p384", "ecdsa-p521":
		return true
	}
	return false
}
