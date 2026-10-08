package runtime

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"os"
)

// TypedError is implemented by Go errors that already know the
// Geblang error class they should surface as (e.g. PermissionError,
// ImmutableError). The VM and evaluator check this via errors.As
// before falling back to heuristics so the original error class
// crosses the native-to-script boundary intact.
type TypedError interface {
	error
	ErrorClass() string
}

type ClassifiedError struct {
	Class   string
	Message string
}

func (e ClassifiedError) Error() string      { return e.Message }
func (e ClassifiedError) ErrorClass() string { return e.Class }

// Returned by cross-module method dispatch when the target method
// doesn't exist, so callers distinguish "missing" from "ran-and-threw".
type MethodNotFoundError struct {
	Class  string
	Method string
}

func (e *MethodNotFoundError) Error() string {
	return "unknown method " + e.Class + "." + e.Method
}

func NewRecoverableError(err error) Error {
	return Error{Class: RecoverableErrorClass(err), Message: recoverableErrorMessage(err)}
}

func RecoverableErrorClass(err error) string {
	var typed TypedError
	if errors.As(err, &typed) {
		return typed.ErrorClass()
	}
	// TimeoutError / TlsError are IOError subclasses, so catch (IOError) still catches them.
	if isTLSError(err) {
		return "TlsError"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "TimeoutError"
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "TimeoutError"
		}
		return "IOError"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return "IOError"
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return "IOError"
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "IOError"
	}
	return "RuntimeError"
}

func isTLSError(err error) bool {
	var verify *tls.CertificateVerificationError
	var unknownAuth x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	var recordHdr tls.RecordHeaderError
	return errors.As(err, &verify) || errors.As(err, &unknownAuth) ||
		errors.As(err, &invalid) || errors.As(err, &hostname) || errors.As(err, &recordHdr)
}

// recoverableErrorMessage strips the typed-error class prefix so a
// PermissionError that already prefixes its message with the class
// name doesn't get the prefix duplicated when the recoverable error
// is later inspected as "<Class>: <Message>".
func recoverableErrorMessage(err error) string {
	var typed TypedError
	if errors.As(err, &typed) {
		msg := err.Error()
		prefix := typed.ErrorClass() + ": "
		if len(msg) > len(prefix) && msg[:len(prefix)] == prefix {
			return msg[len(prefix):]
		}
		return msg
	}
	return err.Error()
}
