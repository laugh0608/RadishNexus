package jenkins

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
)

// DeliveryFailure preserves classification without retaining remote bodies or
// credential-bearing transport errors. Code is always a local allowlisted code.
type DeliveryFailure struct {
	HTTPStatus  int
	Code        string
	Disposition string
}

func (e *DeliveryFailure) Error() string { return e.Code }
func (e *DeliveryFailure) Unwrap() error { return ErrDelivery }

const (
	RetryDelivery = "retry"
	BlockDelivery = "block"
	PauseSource   = "pause"
)

func ClassifyDelivery(err error) DeliveryFailure {
	var failure *DeliveryFailure
	if errors.As(err, &failure) {
		return *failure
	}
	switch {
	case errors.Is(err, ErrResponse):
		return DeliveryFailure{Code: "invalid_delivery_response", Disposition: RetryDelivery}
	case errors.Is(err, ErrDelivery):
		return DeliveryFailure{Code: "delivery_failed", Disposition: RetryDelivery}
	case errors.Is(err, ErrConfiguration):
		return DeliveryFailure{Code: "invalid_configuration", Disposition: PauseSource}
	case errors.Is(err, ErrBinding):
		return DeliveryFailure{Code: "source_binding_mismatch", Disposition: PauseSource}
	case errors.Is(err, ErrUnsupportedResult):
		return DeliveryFailure{Code: "unsupported_result", Disposition: BlockDelivery}
	case errors.Is(err, ErrRunNotCompleted):
		return DeliveryFailure{Code: "run_not_completed", Disposition: BlockDelivery}
	default:
		return DeliveryFailure{Code: "invalid_request", Disposition: BlockDelivery}
	}
}

func certificateFailure(err error) bool {
	var verify *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	return errors.As(err, &verify) || errors.As(err, &authority) || errors.As(err, &hostname) || errors.As(err, &invalid)
}
