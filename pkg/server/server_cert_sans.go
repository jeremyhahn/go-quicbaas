package server

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"slices"
	"strings"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// crlReasonSuperseded is the RFC 5280 CRLReason recorded when a server
// certificate is replaced because its SANs no longer cover the configuration.
const crlReasonSuperseded = 4

// serverCertSANs returns the subject alternative names of the auto-issued
// server TLS certificate: the loopback names, Server.Host, the machine
// hostname and the operator's additional SANs.
//
// The set is de-duplicated and keeps first-seen order. DNS names compare
// case-insensitively and IPs compare by value, so "::1" and
// "0:0:0:0:0:0:0:1" are one entry. A Host or additional DNS name that parses
// as an IP lands in the IP SANs.
func (s *Server) serverCertSANs() *xkmssdk.TrustedCASubjectAlternativeNames {
	var set sanSet
	set.add("localhost")
	set.add("127.0.0.1")
	set.add("::1")
	// When Host is a routable hostname or IP, include it so remote callers
	// can verify this server.
	if host := s.config.Server.Host; host != "" && host != "0.0.0.0" {
		set.add(host)
	}
	// Include the machine hostname so peers can verify this node's certificate.
	if hostname, err := os.Hostname(); err == nil && hostname != "" && hostname != "localhost" {
		set.add(hostname)
	}
	// Operator-supplied additional SANs: required when remote fleet nodes reach
	// this server by an IP or FQDN that differs from Host.
	for _, name := range s.config.Server.AdditionalSANDNS {
		set.add(name)
	}
	for _, ip := range s.config.Server.AdditionalSANIPs {
		set.addIP(ip)
	}
	return &xkmssdk.TrustedCASubjectAlternativeNames{DNS: set.dns, IPs: set.ips}
}

// sanSet accumulates de-duplicated DNS and IP SANs in insertion order.
type sanSet struct {
	dns    []string
	ips    []string
	parsed []net.IP
}

// add routes entry into the IP SANs when it parses as an IP and into the DNS
// SANs otherwise.
func (s *sanSet) add(entry string) {
	if entry == "" {
		return
	}
	if net.ParseIP(entry) != nil {
		s.addIP(entry)
		return
	}
	for _, name := range s.dns {
		if strings.EqualFold(name, entry) {
			return
		}
	}
	s.dns = append(s.dns, entry)
}

// addIP adds entry to the IP SANs in its canonical form. An entry that does
// not parse is kept verbatim: the config validator rejects it, and this
// builder never drops an operator's entry on its own.
func (s *sanSet) addIP(entry string) {
	if entry == "" {
		return
	}
	ip := net.ParseIP(entry)
	if ip == nil {
		if !slices.Contains(s.ips, entry) {
			s.ips = append(s.ips, entry)
		}
		return
	}
	for _, seen := range s.parsed {
		if seen.Equal(ip) {
			return
		}
	}
	s.parsed = append(s.parsed, ip)
	s.ips = append(s.ips, ip.String())
}

// missingSANs returns the SANs in want that cert does not carry.
func missingSANs(cert *x509.Certificate, want *xkmssdk.TrustedCASubjectAlternativeNames) []string {
	var missing []string
	for _, name := range want.DNS {
		covered := slices.ContainsFunc(cert.DNSNames, func(have string) bool {
			return strings.EqualFold(have, name)
		})
		if !covered {
			missing = append(missing, name)
		}
	}
	for _, entry := range want.IPs {
		ip := net.ParseIP(entry)
		covered := ip != nil && slices.ContainsFunc(cert.IPAddresses, ip.Equal)
		if !covered {
			missing = append(missing, entry)
		}
	}
	return missing
}

// issueServerCert issues the server TLS certificate described by request.
//
// When the CA already holds a certificate for the request's Common Name, the
// stored certificate is kept if its SANs cover the request and nil is
// returned. Otherwise the stored certificate is revoked as superseded and
// removed, and a certificate carrying the configured SANs is issued in its
// place over the same key, so a SAN added to the configuration takes effect
// on the next restart.
func (s *Server) issueServerCert(ctx context.Context, ca xkmssdk.TrustedCA, request *xkmssdk.TrustedCACertificateRequest) (*xkmssdk.TrustedCAIssuedCertificate, error) {
	issued, err := ca.IssueCertificate(ctx, request)
	if err == nil {
		return issued, nil
	}
	if !errors.Is(err, xkmssdk.ErrCertificateAlreadyExists) {
		return nil, api.NewOperationFailedError("issue server TLS certificate", err)
	}

	cn := request.Subject.CommonName
	existing, err := ca.CertStore().GetCertificate(cn)
	if err != nil {
		return nil, api.NewOperationFailedError("load existing server TLS certificate", err)
	}
	missing := missingSANs(existing, request.SANS)
	if len(missing) == 0 {
		return nil, nil
	}

	// A previous boot may have revoked the certificate and then failed (or
	// exited) before deleting it. The CA refuses a second revocation, so a
	// certificate it already reports revoked goes straight to deletion.
	revoked, err := ca.IsRevoked(existing.SerialNumber)
	if err != nil {
		return nil, api.NewOperationFailedError("check revocation of superseded server TLS certificate", err)
	}
	if !revoked {
		revoke := xkmssdk.RevokeOptions{Reason: crlReasonSuperseded}
		if err := ca.RevokeCert(ctx, existing, revoke); err != nil {
			return nil, api.NewOperationFailedError("revoke superseded server TLS certificate", err)
		}
	}
	if err := ca.CertStore().DeleteCertificate(cn); err != nil {
		return nil, api.NewOperationFailedError("remove superseded server TLS certificate", err)
	}
	issued, err = ca.IssueCertificate(ctx, request)
	if err != nil {
		return nil, api.NewOperationFailedError("re-issue server TLS certificate", err)
	}
	s.logger.WithFields(map[string]interface{}{
		"cn":         cn,
		"added_sans": strings.Join(missing, ","),
	}).Info("Server TLS certificate re-issued to carry the configured SANs")
	return issued, nil
}
