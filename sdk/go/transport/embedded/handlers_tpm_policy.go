package embedded

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// tpmPolicyServiceName is the SDK service name of the go-xkms
// TPMPolicyService verbs (Security().TPMPolicy()).
const tpmPolicyServiceName = "quicbaas.TPMPolicy"

// tpmPolicyRefusal answers a TPM-policy verb in process. The embedded node
// exposes no TPM-policy service (service.QuicBaaS has no accessor for one),
// so the verb fails with a typed Unimplemented naming the method.
func tpmPolicyRefusal(method string) handlerFunc {
	return func(context.Context, service.QuicBaaS, interface{}) (interface{}, error) {
		return nil, errors.NewUnimplemented("embedded.tpmPolicy."+method,
			"the embedded node has no TPM-policy service; use the REST, QUIC, gRPC or Unix transport")
	}
}
