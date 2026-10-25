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

// init_ceremony.go implements the two verbs that DRIVE the initialization
// ceremony against a running daemon: officer registration and the ceremony
// itself. Together with the existing claim verbs they complete go-qrdb's
// Security Officer distribution model (docs/seal/shamir.md):
//
//	register -> initialize -> claim-share -> quorum unseal
//
// None of this happens at `quicbaasd init` time. The M-of-N barrier is the
// ceremony's job — go-qrdb's bootstrap orchestrator refuses to initialize a
// shamir barrier inline because it needs multiple Security Officers — so the
// daemon boots SEALED and serves these verbs from its sealed window until a
// quorum unseals it.
//
// The security property these verbs preserve: no plaintext share is ever
// written to disk or returned by registration or initialization. Each share
// is sealed to its officer's enrolled key, and only that officer can claim
// and open the envelope.

package main

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// init ceremony flags
// =============================================================================

var (
	initUserPin     string
	initThreshold   int
	initOfficerSpec []string
	initHostname    string
	initSANs        []string
	initCredSeal    string
	initDesignated  string
	initFactors     []string
	initEncKEM      string
	initEncKeyPath  string

	initAdminCSRPath      string
	initAdminCertPath     string
	initAdminCertPoolPath string
	initAdminGenerate     bool
	initAdminKeyOut       string
	initAdminCertOut      string
	initSecret            string
)

// =============================================================================
// Errors
// =============================================================================

// InitOfficerSpecError is returned when a --so value does not match the
// "username:/path/to/csr" form.
type InitOfficerSpecError struct {
	Got string
}

// Error returns a human-readable description.
func (e *InitOfficerSpecError) Error() string {
	return "init: --so value " + e.Got + " must be username:/path/to/file.csr"
}

// InitReadCSRError is returned when an officer CSR file cannot be read.
type InitReadCSRError struct {
	Path string
	Err  error
}

// Error returns a human-readable description.
func (e *InitReadCSRError) Error() string {
	return "init: failed to read CSR " + e.Path + ": " + e.Err.Error()
}

// Unwrap returns the wrapped error.
func (e *InitReadCSRError) Unwrap() error { return e.Err }

// InitAdminSelectorError is returned when 'init initialize' is not given
// exactly one of --admin-csr, --admin-cert or --admin-generate.
type InitAdminSelectorError struct {
	Count int
}

// Error implements the error interface.
func (e *InitAdminSelectorError) Error() string {
	return fmt.Sprintf("init: exactly one of --admin-csr, --admin-cert or --admin-generate is required; %d were set", e.Count)
}

// InitReadAdminFileError is returned when an admin credential file named by
// an 'init initialize' flag cannot be read.
type InitReadAdminFileError struct {
	Flag string
	Path string
	Err  error
}

// Error implements the error interface.
func (e *InitReadAdminFileError) Error() string {
	return "init: failed to read --" + e.Flag + " " + e.Path + ": " + e.Err.Error()
}

// Unwrap returns the underlying read error.
func (e *InitReadAdminFileError) Unwrap() error { return e.Err }

// InitAdminOutputRequiredError is returned when 'init initialize
// --admin-generate' is run without a file for the generated credential. The
// daemon returns the generated private key once and never stores it, so the
// command refuses to call it without a destination.
type InitAdminOutputRequiredError struct {
	Flag string
}

// Error implements the error interface.
func (e *InitAdminOutputRequiredError) Error() string {
	return "init: --admin-generate requires --" + e.Flag
}

// InitAdminOutputFileError is returned when a file for the generated
// administrator credential cannot be created, written or removed.
type InitAdminOutputFileError struct {
	Flag string
	Path string
	Op   string
	Err  error
}

// Error implements the error interface.
func (e *InitAdminOutputFileError) Error() string {
	return "init: failed to " + e.Op + " --" + e.Flag + " " + e.Path + ": " + e.Err.Error()
}

// Unwrap returns the underlying file error.
func (e *InitAdminOutputFileError) Unwrap() error { return e.Err }

// InitAdminCredentialMissingError is returned when the daemon's response to
// --admin-generate carries no administrator key or certificate.
type InitAdminCredentialMissingError struct {
	Field string
}

// Error implements the error interface.
func (e *InitAdminCredentialMissingError) Error() string {
	return "init: the daemon returned no " + e.Field + " for --admin-generate"
}

// =============================================================================
// initialize
// =============================================================================

// readInitAdminCredential checks that exactly one admin credential selector
// flag is set and loads the PEM files it names into req.
func readInitAdminCredential(req *qrdbtransport.CeremonyInitializeRequest) error {
	count := 0
	for _, set := range []bool{initAdminCSRPath != "", initAdminCertPath != "", initAdminGenerate} {
		if set {
			count++
		}
	}
	if count != 1 {
		return &InitAdminSelectorError{Count: count}
	}

	files := []struct {
		flag string
		path string
		dst  *string
	}{
		{"admin-csr", initAdminCSRPath, &req.AdminCSRPEM},
		{"admin-cert", initAdminCertPath, &req.AdminCertPEM},
		{"admin-cert-pool", initAdminCertPoolPath, &req.AdminCertPoolPEM},
	}
	for _, f := range files {
		if f.path == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Clean(f.path))
		if err != nil {
			return &InitReadAdminFileError{Flag: f.flag, Path: f.path, Err: err}
		}
		*f.dst = string(data)
	}
	return nil
}

// initAdminOutputFile is one new file the generated administrator credential
// is written to.
type initAdminOutputFile struct {
	flag string
	path string
	file *os.File
}

// initAdminOutputs holds the files --admin-generate writes the administrator
// key and certificate to. Both are nil when --admin-generate is not set.
type initAdminOutputs struct {
	key  *initAdminOutputFile
	cert *initAdminOutputFile
}

// openInitAdminOutputs creates the --admin-key-out and --admin-cert-out files
// before the daemon is called, so a run that could not keep the generated
// key never asks for one. Each file must not exist yet; the key file is
// created with mode 0600.
func openInitAdminOutputs() (initAdminOutputs, error) {
	if !initAdminGenerate {
		return openInitAdminCSROutput()
	}
	if initAdminKeyOut == "" {
		return initAdminOutputs{}, &InitAdminOutputRequiredError{Flag: "admin-key-out"}
	}
	if initAdminCertOut == "" {
		return initAdminOutputs{}, &InitAdminOutputRequiredError{Flag: "admin-cert-out"}
	}

	key, err := createInitAdminOutputFile("admin-key-out", initAdminKeyOut, 0o600)
	if err != nil {
		return initAdminOutputs{}, err
	}
	cert, err := createInitAdminOutputFile("admin-cert-out", initAdminCertOut, 0o644)
	if err != nil {
		return initAdminOutputs{}, stderrors.Join(err, key.discard())
	}
	return initAdminOutputs{key: key, cert: cert}, nil
}

// openInitAdminCSROutput creates the optional --admin-cert-out file for
// --admin-csr, which receives the certificate the daemon issues for the CSR.
// Without the flag the certificate is printed instead.
func openInitAdminCSROutput() (initAdminOutputs, error) {
	if initAdminCSRPath == "" || initAdminCertOut == "" {
		return initAdminOutputs{}, nil
	}
	cert, err := createInitAdminOutputFile("admin-cert-out", initAdminCertOut, 0o644)
	if err != nil {
		return initAdminOutputs{}, err
	}
	return initAdminOutputs{cert: cert}, nil
}

// createInitAdminOutputFile creates path exclusively with the given mode.
func createInitAdminOutputFile(flag, path string, mode os.FileMode) (*initAdminOutputFile, error) {
	f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return nil, &InitAdminOutputFileError{Flag: flag, Path: path, Op: "create", Err: err}
	}
	return &initAdminOutputFile{flag: flag, path: path, file: f}, nil
}

// write writes data to the file and closes it.
func (o *initAdminOutputFile) write(data []byte) error {
	if _, err := o.file.Write(data); err != nil {
		return stderrors.Join(
			&InitAdminOutputFileError{Flag: o.flag, Path: o.path, Op: "write", Err: err},
			o.file.Close())
	}
	if err := o.file.Close(); err != nil {
		return &InitAdminOutputFileError{Flag: o.flag, Path: o.path, Op: "write", Err: err}
	}
	return nil
}

// discard closes and removes a file that was created but never written.
func (o *initAdminOutputFile) discard() error {
	closeErr := o.file.Close()
	if err := os.Remove(filepath.Clean(o.path)); err != nil {
		return stderrors.Join(closeErr,
			&InitAdminOutputFileError{Flag: o.flag, Path: o.path, Op: "remove", Err: err})
	}
	return closeErr
}

// discard removes the opened files after the daemon call failed, so the
// operator can retry with the same paths. It is a no-op when none was opened.
func (o initAdminOutputs) discard() error {
	var errs []error
	for _, f := range []*initAdminOutputFile{o.key, o.cert} {
		if f != nil {
			errs = append(errs, f.discard())
		}
	}
	return stderrors.Join(errs...)
}

// paths returns the key and certificate file paths, each empty when that
// file was not opened.
func (o initAdminOutputs) paths() (key, cert string) {
	if o.key != nil {
		key = o.key.path
	}
	if o.cert != nil {
		cert = o.cert.path
	}
	return key, cert
}

// store writes data to the file, or removes the file when the daemon
// returned nothing for it.
func (o *initAdminOutputFile) store(data []byte, field string) error {
	if len(data) == 0 {
		return stderrors.Join(&InitAdminCredentialMissingError{Field: field}, o.discard())
	}
	return o.write(data)
}

// write stores the administrator key and certificate in the opened files.
// Each is written independently, so a response missing one never costs the
// other.
func (o initAdminOutputs) write(resp *qrdbtransport.CeremonyInitializeResponse) error {
	var errs []error
	if o.key != nil {
		errs = append(errs, o.key.store(resp.AdminKeyPEM, "administrator key"))
	}
	if o.cert != nil {
		errs = append(errs, o.cert.store([]byte(resp.AdminCertPEM), "administrator certificate"))
	}
	return stderrors.Join(errs...)
}

// newInitInitializeCommand builds 'quicbaas init initialize'.
func newInitInitializeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "initialize",
		Short: "Run the initialization ceremony against a running daemon",
		Long: `Run the initialization ceremony.

With --threshold and one --so per officer this performs the M-of-N Shamir
initialization: the barrier secret is split and ONE SHARE IS SEALED PER
OFFICER into a memguard enclave inside the daemon. No share is returned by
this call and no share is written to disk. Each officer then claims theirs
with 'quicbaas init claim-share'.

Register each officer first so their CSR is signed and bound:
  quicbaas init officer register --username alice --csr alice.csr

Exactly one admin credential selector is required: --admin-csr signs the
admin's own CSR, --admin-cert binds an existing admin certificate (with
--admin-cert-pool naming the CA bundle that issued it), and --admin-generate
has the daemon generate the admin keypair and certificate. The daemon
returns the generated private key exactly once and never stores it, so
--admin-generate requires --admin-key-out and --admin-cert-out: both must name
new files, which are created (the key with mode 0600) before the daemon is
called, and the command refuses to run if either already exists. With
--admin-csr the issued certificate is written to --admin-cert-out when given,
otherwise printed. The certificate issued to each --so holder is printed,
labelled with the holder, since initialization cannot be repeated. The barrier
passphrase is read from --passphrase-stdin, QUICBAAS_BARRIER_PASSPHRASE or
--passphrase when the barrier protection needs one.

Example:
  quicbaas init initialize \
      --so-pin "$SO_PIN" \
      --user-pin "$USER_PIN" \
      --admin-csr admin.csr \
      --threshold 2 \
      --so alice:alice.csr \
      --so bob:bob.csr \
      --so carol:carol.csr`,
		RunE: runInitInitializeCommand,
	}
	cmd.Flags().StringVar(&initServer, "server", "",
		"Override the global --address for this call (optional)")
	cmd.Flags().StringVar(&initSOPin, "so-pin", "",
		"Security Officer PIN (required)")
	cmd.Flags().StringVar(&initUserPin, "user-pin", "",
		"User PIN (required)")
	cmd.Flags().IntVar(&initThreshold, "threshold", 0,
		"Shamir M-of-N threshold. Omit or set below 2 for single-admin mode")
	cmd.Flags().StringSliceVar(&initOfficerSpec, "so", nil,
		"Officer as username:/path/to/file.csr (repeatable, required for M-of-N)")
	cmd.Flags().StringVar(&initHostname, "hostname", "",
		"FQDN stamped into the issued serving certificate")
	cmd.Flags().StringSliceVar(&initSANs, "sans", nil,
		"Additional DNS / IP SANs for the issued serving certificate")
	cmd.Flags().StringVar(&initCredSeal, "credential-seal-strategy", "",
		"Credential seal strategy (manual | barrier); empty uses the server default")
	cmd.Flags().StringVar(&initAdminCSRPath, "admin-csr", "",
		"Path to the admin CSR the ceremony CA signs")
	cmd.Flags().StringVar(&initAdminCertPath, "admin-cert", "",
		"Path to an existing admin certificate to bind")
	cmd.Flags().StringVar(&initAdminCertPoolPath, "admin-cert-pool", "",
		"Path to the CA bundle that issued --admin-cert")
	cmd.Flags().BoolVar(&initAdminGenerate, "admin-generate", false,
		"Have the daemon generate the admin keypair and certificate")
	cmd.Flags().StringVar(&initAdminKeyOut, "admin-key-out", "",
		"New file (mode 0600) the --admin-generate private key is written to (required with --admin-generate)")
	cmd.Flags().StringVar(&initAdminCertOut, "admin-cert-out", "",
		"New file the --admin-generate or --admin-csr certificate is written to (required with --admin-generate)")
	addPassphraseFlags(cmd, &initSecret, "System barrier passphrase")
	return cmd
}

// runInitInitializeCommand executes 'init initialize'.
func runInitInitializeCommand(cmd *cobra.Command, _ []string) error {
	if initSOPin == "" {
		return errors.Newf(errors.ErrInvalidInput, "init.initialize", "--so-pin is required")
	}
	if initUserPin == "" {
		return errors.Newf(errors.ErrInvalidInput, "init.initialize", "--user-pin is required")
	}

	officers, err := parseCeremonyOfficers(initOfficerSpec)
	if err != nil {
		return formatError(err)
	}
	if initThreshold >= 2 && len(officers) < initThreshold {
		return errors.Newf(errors.ErrInvalidInput, "init.initialize",
			"--threshold %d requires at least %d --so officers, got %d",
			initThreshold, initThreshold, len(officers))
	}

	req := &qrdbtransport.CeremonyInitializeRequest{
		SOPin:                  initSOPin,
		UserPin:                initUserPin,
		CredentialSealStrategy: initCredSeal,
		Threshold:              initThreshold,
		Officers:               officers,
		Hostname:               initHostname,
		SANs:                   initSANs,
		AdminGenerate:          initAdminGenerate,
	}
	if err := readInitAdminCredential(req); err != nil {
		return formatError(err)
	}

	secret, err := resolvePassphrase(cmd, "init.initialize", initSecret)
	if err != nil {
		return formatError(err)
	}
	defer secret.Destroy()

	outputs, err := openInitAdminOutputs()
	if err != nil {
		return formatError(err)
	}

	client, err := ceremonyClientFactory()
	if err != nil {
		return formatError(stderrors.Join(err, outputs.discard()))
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	var resp *qrdbtransport.CeremonyInitializeResponse
	err = withPassphraseBytes(secret, func(b []byte) error {
		req.Secret = b
		var callErr error
		resp, callErr = client.CeremonyInitialize(ctx, req)
		return callErr
	})
	if err != nil {
		return formatError(stderrors.Join(err, outputs.discard()))
	}
	if resp == nil {
		return formatError(stderrors.Join(
			errors.Newf(errors.ErrInternal, "init.initialize", "empty response"),
			outputs.discard()))
	}
	if err := outputs.write(resp); err != nil {
		return formatError(err)
	}

	keyPath, certPath := outputs.paths()
	return formatCeremonyInitializeResponse(resp, keyPath, certPath, initOutputFmt, cmd.OutOrStdout())
}

// parseCeremonyOfficers turns each "username:/path/to/file.csr" value into a
// ceremony officer request carrying the PEM CSR.
//
// The CSR travels to the server, which signs it and binds the officer to the
// issued certificate's thumbprint. Only the officer holds the matching private
// key, which is what makes the later share claim provable — and what keeps the
// server from ever being able to impersonate an officer.
func parseCeremonyOfficers(specs []string) ([]qrdbtransport.CeremonyOfficerRequest, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	officers := make([]qrdbtransport.CeremonyOfficerRequest, 0, len(specs))
	for _, spec := range specs {
		username, path, found := strings.Cut(spec, ":")
		if !found || username == "" || path == "" {
			return nil, &InitOfficerSpecError{Got: spec}
		}
		csrPEM, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, &InitReadCSRError{Path: path, Err: err}
		}
		officers = append(officers, qrdbtransport.CeremonyOfficerRequest{
			Username: username,
			CSRPEM:   string(csrPEM),
		})
	}
	return officers, nil
}

// =============================================================================
// officer
// =============================================================================

// newInitOfficerCommand builds the 'quicbaas init officer' group.
func newInitOfficerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "officer",
		Short: "Manage Security Officer enrolment",
		Long: `Manage Security Officer enrolment.

Registration is step 1 of the Shamir flow: the admin submits each officer's
CSR, the ceremony CA signs it, and the officer is bound to the SHA-256
thumbprint of the issued certificate. The response carries the signed
certificate and the officer's enrolled factors — never share bytes.
'officer list' reports each officer's enrolment and claim state.`,
	}
	cmd.AddCommand(newInitOfficerRegisterCommand())
	cmd.AddCommand(newInitOfficerListCommand())
	return cmd
}

// newInitOfficerRegisterCommand builds 'quicbaas init officer register'.
func newInitOfficerRegisterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register a Security Officer from their CSR",
		Long: `Register a Security Officer.

The officer generates their own keypair and CSR — ideally on a security key
or PIV smartcard, so the private key never exists as a file. This command
submits only the CSR. The issued certificate is written to --output for
delivery to that officer.

--domain recovery enrols a recovery officer instead of an unseal officer.
--designated names the factor fixed at enrolment under the designated policy
(mtls when empty); --factor enrols an additional factor, as a name or
name=<json params>. --encryption-kem and --encryption-key-file enrol a
separate envelope key; the certificate key is used when they are empty.

Example:
  quicbaas init officer register \
      --username alice \
      --csr alice.csr \
      --designated mtls \
      --output alice.crt`,
		RunE: runInitOfficerRegisterCommand,
	}
	cmd.Flags().StringVar(&initServer, "server", "",
		"Override the global --address for this call (optional)")
	cmd.Flags().StringVar(&initUsername, "username", "",
		"Officer username (required)")
	cmd.Flags().StringVar(&initCSRPath, "csr", "",
		"Path to the officer's PEM-encoded CSR (required)")
	cmd.Flags().StringVar(&initOutputPath, "output", "",
		"Path where the issued officer certificate should be written (optional)")
	cmd.Flags().StringVar(&initRole, "role", qrdbtransport.RoleSecurityOfficer,
		"Holder role: so or custodian")
	cmd.Flags().StringVar(&initDesignated, "designated", "",
		"Factor fixed at enrolment under the designated policy (default mtls)")
	cmd.Flags().StringArrayVar(&initFactors, "factor", nil,
		"Additional factor enrolment: name or name=<json params> (repeatable)")
	cmd.Flags().StringVar(&initEncKEM, "encryption-kem", "",
		"HPKE suite of a separate envelope key (requires --encryption-key-file)")
	cmd.Flags().StringVar(&initEncKeyPath, "encryption-key-file", "",
		"Raw public key of the separate envelope key, in the suite's encoding")
	addBarrierDomainFlag(cmd)
	return cmd
}

// runInitOfficerRegisterCommand executes 'init officer register'.
func runInitOfficerRegisterCommand(cmd *cobra.Command, _ []string) error {
	if initUsername == "" {
		return errors.Newf(errors.ErrInvalidInput, "init.officer.register", "--username is required")
	}
	if initCSRPath == "" {
		return errors.Newf(errors.ErrInvalidInput, "init.officer.register", "--csr is required")
	}
	if err := validateBarrierQuorumDomain("init.officer.register", barrierDomain); err != nil {
		return err
	}
	if (initEncKEM == "") != (initEncKeyPath == "") {
		return errors.Newf(errors.ErrInvalidInput, "init.officer.register",
			"--encryption-kem and --encryption-key-file must be supplied together")
	}
	factors, err := parseHolderFactors(initFactors)
	if err != nil {
		return err
	}

	csrPEM, err := os.ReadFile(filepath.Clean(initCSRPath))
	if err != nil {
		return formatError(&InitReadCSRError{Path: initCSRPath, Err: err})
	}
	var encKey []byte
	if initEncKeyPath != "" {
		encKey, err = os.ReadFile(filepath.Clean(initEncKeyPath))
		if err != nil {
			return formatError(&InitReadCSRError{Path: initEncKeyPath, Err: err})
		}
	}

	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	register := client.OfficerRegister
	if barrierDomain == qrdbtransport.DomainRecovery {
		register = client.RecoveryOfficerRegister
	}
	resp, err := register(ctx, &qrdbtransport.OfficerRegisterRequest{
		Username:      initUsername,
		Role:          initRole,
		CSRPEM:        string(csrPEM),
		EncryptionKEM: initEncKEM,
		EncryptionKey: encKey,
		Factors:       factors,
		Designated:    initDesignated,
	})
	if err != nil {
		return formatError(err)
	}

	if initOutputPath != "" && len(resp.CertPEM) > 0 {
		if werr := writeFileSecure(initOutputPath, []byte(resp.CertPEM)); werr != nil {
			return formatError(&InitWriteFileError{Path: initOutputPath, Err: werr})
		}
	}

	return formatOfficerRegisterResponse(resp, initUsername, initOutputPath, initOutputFmt, cmd.OutOrStdout())
}

// parseHolderFactors turns each --factor value, a factor name or
// name=<json params>, into the enrolment go-qrdb expects.
func parseHolderFactors(specs []string) ([]qrdbtransport.HolderFactorEnrolment, error) {
	factors := make([]qrdbtransport.HolderFactorEnrolment, 0, len(specs))
	for _, spec := range specs {
		name, rawParams, hasParams := strings.Cut(spec, "=")
		if name == "" {
			return nil, errors.Newf(errors.ErrInvalidInput, "init.officer.register",
				"--factor %q has no factor name", spec)
		}
		factor := qrdbtransport.HolderFactorEnrolment{Name: name}
		if hasParams {
			if err := json.Unmarshal([]byte(rawParams), &factor.Params); err != nil {
				return nil, errors.Wrapf(err, errors.ErrInvalidInput, "init.officer.register",
					"--factor %s params are not a JSON object", name)
			}
		}
		factors = append(factors, factor)
	}
	return factors, nil
}

// newInitOfficerListCommand builds 'quicbaas init officer list'.
func newInitOfficerListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the enrolled Security Officers",
		Long: `List the enrolled Security Officers.

Only public metadata is returned: username, certificate thumbprint, share
index, auth factor, and enrolment time. No share material is ever included.`,
		RunE: runInitOfficerListCommand,
	}
	cmd.Flags().StringVar(&initServer, "server", "",
		"Override the global --address for this call (optional)")
	return cmd
}

// runInitOfficerListCommand executes 'init officer list'.
func runInitOfficerListCommand(cmd *cobra.Command, _ []string) error {
	client, err := barrierClientFactory()
	if err != nil {
		return formatError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), globalTimeout)
	defer cancel()

	officers, err := client.OfficerList(ctx)
	if err != nil {
		return formatError(err)
	}
	return formatOfficerStatusList(officers, initOutputFmt, cmd.OutOrStdout())
}

// =============================================================================
// Output formatting
// =============================================================================

// formatCeremonyInitializeResponse renders the ceremony result. The share
// count is reported so the operator can confirm the split happened, but the
// shares themselves are not here and never will be — they are held in the
// daemon until each officer claims theirs.
func formatCeremonyInitializeResponse(
	resp *qrdbtransport.CeremonyInitializeResponse,
	adminKeyPath, adminCertPath, format string,
	w io.Writer,
) error {
	if resp == nil {
		return errors.Newf(errors.ErrInternal, "init.initialize", "empty response")
	}
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if outputFormat == cli.OutputFormatJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(resp)
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "CEREMONY INITIALIZED\n")
	fmt.Fprintf(tw, "%s\n", strings.Repeat("=", 60))
	fmt.Fprintf(tw, "State:\t%s\n", resp.State)
	if resp.SPKIPin != "" {
		fmt.Fprintf(tw, "SPKI Pin:\t%s\n", resp.SPKIPin)
	}
	if resp.ShareCount > 0 {
		fmt.Fprintf(tw, "Shares:\t%d generated\n", resp.ShareCount)
	}
	if adminKeyPath != "" {
		fmt.Fprintf(tw, "Admin Key:\t%s\n", adminKeyPath)
	}
	if adminCertPath != "" {
		fmt.Fprintf(tw, "Admin Certificate:\t%s\n", adminCertPath)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	// Initialization cannot be repeated: every issued certificate that was
	// not saved to a file is printed, or the operator never receives it.
	if adminCertPath == "" && resp.AdminCertPEM != "" {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Admin Certificate:\n%s", resp.AdminCertPEM)
	}
	for _, holder := range resp.Holders {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Holder %s (%s) Certificate:\n%s", holder.Username, holder.Role, holder.CertPEM)
	}
	if resp.ShareCount > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Each share is sealed to its officer, who claims the envelope with:")
		fmt.Fprintln(w, "  quicbaas init claim-share --username <name> --tls-cert <cert> --tls-key <key> --output <envelope>")
	}
	return nil
}

// formatOfficerRegisterResponse renders one officer registration.
func formatOfficerRegisterResponse(
	resp *qrdbtransport.OfficerRegisterResponse,
	username, outputPath, format string,
	w io.Writer,
) error {
	if resp == nil {
		return errors.Newf(errors.ErrInternal, "init.officer.register", "empty response")
	}
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if outputFormat == cli.OutputFormatJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(map[string]interface{}{
			"username":       username,
			"role":           resp.Role,
			"designated":     resp.Designated,
			"factors":        resp.Factors,
			"encryption_kem": resp.EncryptionKEM,
			"cert_pem":       resp.CertPEM,
		})
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Officer:\t%s\n", username)
	fmt.Fprintf(tw, "Role:\t%s\n", resp.Role)
	if resp.Designated != "" {
		fmt.Fprintf(tw, "Designated Factor:\t%s\n", resp.Designated)
	}
	if len(resp.Factors) > 0 {
		fmt.Fprintf(tw, "Factors:\t%s\n", strings.Join(resp.Factors, ", "))
	}
	if outputPath != "" {
		fmt.Fprintf(tw, "Certificate:\t%s\n", outputPath)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if outputPath == "" && len(resp.CertPEM) > 0 {
		fmt.Fprintln(w)
		fmt.Fprint(w, resp.CertPEM)
	}
	return nil
}

// formatOfficerStatusList renders the officer roster.
func formatOfficerStatusList(
	officers []*qrdbtransport.OfficerStatus,
	format string,
	w io.Writer,
) error {
	outputFormat, err := cli.ParseOutputFormat(format)
	if err != nil {
		return err
	}
	if outputFormat == cli.OutputFormatJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(officers)
	}
	if len(officers) == 0 {
		fmt.Fprintln(w, "No Security Officers enrolled.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "USERNAME\tROLE\tINDEX\tDESIGNATED\tCLAIMED\tTHUMBPRINT\n")
	for _, o := range officers {
		if o == nil {
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%t\t%s\n",
			o.Username, o.Role, o.ShareIndex, o.Designated, !o.ClaimedAt.IsZero(), o.CertThumbprint)
	}
	return tw.Flush()
}
