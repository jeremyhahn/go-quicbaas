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

package embedded

import (
	"context"
	"reflect"

	certclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Certificate Handlers
// =============================================================================

func handleCertIssueCertificate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	certSvc := svc.Certificate()
	if certSvc == nil {
		return nil, NewServiceNotConfiguredError("Certificate")
	}

	// Accept service type directly
	if r, ok := req.(*service.IssueCertificateRequest); ok {
		return certSvc.IssueCertificate(ctx, r)
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*certclient.IssueCertificateRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.Certificate", "IssueCertificate",
			"*service.IssueCertificateRequest or *certificate.IssueCertificateRequest",
			reflect.TypeOf(req).String(),
		)
	}

	svcReq := &service.IssueCertificateRequest{
		CommonName:         sdkReq.CommonName,
		Organization:       sdkReq.Organization,
		OrganizationalUnit: sdkReq.OrganizationalUnit,
		Country:            sdkReq.Country,
		Province:           sdkReq.Province,
		Locality:           sdkReq.Locality,
		DNSNames:           sdkReq.DNSNames,
		IPAddresses:        sdkReq.IPAddresses,
		EmailAddresses:     sdkReq.EmailAddresses,
		URIs:               sdkReq.URIs,
		ValidityDays:       sdkReq.ValidityDays,
		Usage:              sdkReq.Usage,
		IsCA:               sdkReq.IsCA,
		MaxPathLen:         sdkReq.MaxPathLen,
		KeyAlgorithm:       sdkReq.KeyAlgorithm,
		KeySize:            sdkReq.KeySize,
		Metadata:           sdkReq.Metadata,
	}
	return certSvc.IssueCertificate(ctx, svcReq)
}

func handleCertSignCSR(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	certSvc := svc.Certificate()
	if certSvc == nil {
		return nil, NewServiceNotConfiguredError("Certificate")
	}

	// Accept service type directly
	if r, ok := req.(*service.SignCSRRequest); ok {
		return certSvc.SignCSR(ctx, r)
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*certclient.SignCSRRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.Certificate", "SignCSR",
			"*service.SignCSRRequest or *certificate.SignCSRRequest",
			reflect.TypeOf(req).String(),
		)
	}

	svcReq := &service.SignCSRRequest{
		CSRPEM:       sdkReq.CSRPEM,
		ValidityDays: sdkReq.ValidityDays,
		Usage:        sdkReq.Usage,
	}
	resp, err := certSvc.SignCSR(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	// An SDK caller gets the SDK response type back; the certificate client
	// rejects any other type.
	sdkResp := certclient.SignCSRResponse(*resp)
	return &sdkResp, nil
}

func handleCertRevokeCertificate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	certSvc := svc.Certificate()
	if certSvc == nil {
		return nil, NewServiceNotConfiguredError("Certificate")
	}

	// Accept service type directly
	if r, ok := req.(*service.RevokeCertificateRequest); ok {
		return certSvc.RevokeCertificate(ctx, r)
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*certclient.RevokeCertificateRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.Certificate", "RevokeCertificate",
			"*service.RevokeCertificateRequest or *certificate.RevokeCertificateRequest",
			reflect.TypeOf(req).String(),
		)
	}

	svcReq := &service.RevokeCertificateRequest{
		SerialNumber: sdkReq.SerialNumber,
	}
	return certSvc.RevokeCertificate(ctx, svcReq)
}

func handleCertVerifyCertificate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	certSvc := svc.Certificate()
	if certSvc == nil {
		return nil, NewServiceNotConfiguredError("Certificate")
	}

	// Accept service type directly
	if r, ok := req.(*service.VerifyCertificateRequest); ok {
		return certSvc.VerifyCertificate(ctx, r)
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*certclient.VerifyCertificateRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.Certificate", "VerifyCertificate",
			"*service.VerifyCertificateRequest or *certificate.VerifyCertificateRequest",
			reflect.TypeOf(req).String(),
		)
	}

	svcReq := &service.VerifyCertificateRequest{
		CertificatePEM: sdkReq.CertificatePEM,
	}
	return certSvc.VerifyCertificate(ctx, svcReq)
}

func handleCertGetCACertificate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	certSvc := svc.Certificate()
	if certSvc == nil {
		return nil, NewServiceNotConfiguredError("Certificate")
	}

	// Accept service type directly
	if r, ok := req.(*service.GetCACertificateRequest); ok {
		return certSvc.GetCACertificate(ctx, r)
	}

	// Accept SDK client type (no fields to convert)
	if _, ok := req.(*certclient.GetCACertificateRequest); ok {
		return certSvc.GetCACertificate(ctx, &service.GetCACertificateRequest{})
	}

	return nil, NewInvalidRequestTypeError(
		"quicbaas.Certificate", "GetCACertificate",
		"*service.GetCACertificateRequest or *certificate.GetCACertificateRequest",
		reflect.TypeOf(req).String(),
	)
}

func handleCertGetCABundle(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	certSvc := svc.Certificate()
	if certSvc == nil {
		return nil, NewServiceNotConfiguredError("Certificate")
	}

	// Accept service type directly
	if r, ok := req.(*service.GetCABundleRequest); ok {
		return certSvc.GetCABundle(ctx, r)
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*certclient.GetCABundleRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.Certificate", "GetCABundle",
			"*service.GetCABundleRequest or *certificate.GetCABundleRequest",
			reflect.TypeOf(req).String(),
		)
	}

	svcReq := &service.GetCABundleRequest{
		StoreType: sdkReq.StoreType,
		Algorithm: sdkReq.Algorithm,
	}
	return certSvc.GetCABundle(ctx, svcReq)
}

func handleCertGetTLSConfig(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	certSvc := svc.Certificate()
	if certSvc == nil {
		return nil, NewServiceNotConfiguredError("Certificate")
	}

	// Accept service type directly
	if r, ok := req.(*service.GetTLSConfigRequest); ok {
		return certSvc.GetTLSConfig(ctx, r)
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*certclient.GetTLSConfigRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.Certificate", "GetTLSConfig",
			"*service.GetTLSConfigRequest or *certificate.GetTLSConfigRequest",
			reflect.TypeOf(req).String(),
		)
	}

	svcReq := &service.GetTLSConfigRequest{
		ServerName:        sdkReq.ServerName,
		RequireClientCert: sdkReq.RequireClientCert,
		IsServer:          sdkReq.IsServer,
		MinVersion:        sdkReq.MinVersion,
	}
	return certSvc.GetTLSConfig(ctx, svcReq)
}

func handleCertIsInitialized(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	certSvc := svc.Certificate()
	if certSvc == nil {
		return nil, NewServiceNotConfiguredError("Certificate")
	}

	initialized := certSvc.IsInitialized()
	return &certclient.IsInitializedResponse{
		Initialized: initialized,
	}, nil
}

func handleCertGenerateCRL(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	certSvc := svc.Certificate()
	if certSvc == nil {
		return nil, NewServiceNotConfiguredError("Certificate")
	}

	// Accept service type directly
	if r, ok := req.(*service.GenerateCRLRequest); ok {
		return certSvc.GenerateCRL(ctx, r)
	}

	// Accept SDK client type (no fields to convert)
	if _, ok := req.(*certclient.GenerateCRLRequest); ok {
		return certSvc.GenerateCRL(ctx, &service.GenerateCRLRequest{})
	}

	return nil, NewInvalidRequestTypeError(
		"quicbaas.Certificate", "GenerateCRL",
		"*service.GenerateCRLRequest or *certificate.GenerateCRLRequest",
		reflect.TypeOf(req).String(),
	)
}

func handleCertIsRevoked(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	certSvc := svc.Certificate()
	if certSvc == nil {
		return nil, NewServiceNotConfiguredError("Certificate")
	}

	// Accept service type directly
	if r, ok := req.(*service.IsRevokedRequest); ok {
		return certSvc.IsRevoked(ctx, r)
	}

	// Accept SDK client type and convert
	sdkReq, ok := req.(*certclient.IsRevokedRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.Certificate", "IsRevoked",
			"*service.IsRevokedRequest or *certificate.IsRevokedRequest",
			reflect.TypeOf(req).String(),
		)
	}

	svcReq := &service.IsRevokedRequest{
		SerialNumber: sdkReq.SerialNumber,
	}
	return certSvc.IsRevoked(ctx, svcReq)
}

func handleCertCAInfo(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	certSvc := svc.Certificate()
	if certSvc == nil {
		return nil, NewServiceNotConfiguredError("Certificate")
	}

	// Accept service type directly.
	if r, ok := req.(*service.CAInfoRequest); ok {
		svcResp, err := certSvc.CAInfo(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertCAInfoResponse(svcResp), nil
	}

	// Accept SDK client type.
	if _, ok := req.(*certclient.CAInfoRequest); ok {
		svcResp, err := certSvc.CAInfo(ctx, &service.CAInfoRequest{})
		if err != nil {
			return nil, err
		}
		return convertCAInfoResponse(svcResp), nil
	}

	return nil, NewInvalidRequestTypeError(
		"quicbaas.Certificate", "CAInfo",
		"*service.CAInfoRequest or *certificate.CAInfoRequest",
		reflect.TypeOf(req).String(),
	)
}

func handleCertListCertificates(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	certSvc := svc.Certificate()
	if certSvc == nil {
		return nil, NewServiceNotConfiguredError("Certificate")
	}

	// Accept service type directly.
	if r, ok := req.(*service.ListCertificatesRequest); ok {
		svcResp, err := certSvc.ListCertificates(ctx, r)
		if err != nil {
			return nil, err
		}
		return convertListCertificatesResponse(svcResp), nil
	}

	// Accept SDK client type and convert.
	sdkReq, ok := req.(*certclient.ListCertificatesRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError(
			"quicbaas.Certificate", "ListCertificates",
			"*service.ListCertificatesRequest or *certificate.ListCertificatesRequest",
			reflect.TypeOf(req).String(),
		)
	}
	svcReq := &service.ListCertificatesRequest{
		CommonNameFilter: sdkReq.CommonNameFilter,
		IncludeRevoked:   sdkReq.IncludeRevoked,
		IncludeExpired:   sdkReq.IncludeExpired,
		Limit:            sdkReq.Limit,
	}
	svcResp, err := certSvc.ListCertificates(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	return convertListCertificatesResponse(svcResp), nil
}

// convertCAInfoResponse translates the service-layer response into the
// SDK wire type returned over the embedded transport.
func convertCAInfoResponse(svcResp *service.CAInfoResponse) *certclient.CAInfoResponse {
	if svcResp == nil {
		return nil
	}
	return &certclient.CAInfoResponse{
		CommonName:         svcResp.CommonName,
		Subject:            svcResp.Subject,
		Issuer:             svcResp.Issuer,
		SerialNumber:       svcResp.SerialNumber,
		NotBefore:          svcResp.NotBefore,
		NotAfter:           svcResp.NotAfter,
		IsCA:               svcResp.IsCA,
		KeyType:            svcResp.KeyType,
		KeyBits:            svcResp.KeyBits,
		SignatureAlgorithm: svcResp.SignatureAlgorithm,
		CertificatePEM:     svcResp.CertificatePEM,
		CABundlePEM:        svcResp.CABundlePEM,
	}
}

// convertListCertificatesResponse translates the service-layer response
// into the SDK wire type returned over the embedded transport.
func convertListCertificatesResponse(svcResp *service.ListCertificatesResponse) *certclient.ListCertificatesResponse {
	if svcResp == nil {
		return nil
	}
	out := &certclient.ListCertificatesResponse{
		Certificates: make([]certclient.CertificateSummary, 0, len(svcResp.Certificates)),
		TotalCount:   svcResp.TotalCount,
	}
	for _, c := range svcResp.Certificates {
		out.Certificates = append(out.Certificates, certclient.CertificateSummary{
			CommonName:   c.CommonName,
			Subject:      c.Subject,
			Issuer:       c.Issuer,
			SerialNumber: c.SerialNumber,
			NotBefore:    c.NotBefore,
			NotAfter:     c.NotAfter,
			IsCA:         c.IsCA,
			KeyUsage:     c.KeyUsage,
			DNSNames:     append([]string(nil), c.DNSNames...),
		})
	}
	return out
}
