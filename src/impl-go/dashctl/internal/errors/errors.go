// Package errors defines dashctl's stable error model. Every command emits
// errors classified into a documented set of exit codes (see [Code]).
// The classifier converts transport-level errors (HTTP status, gRPC code,
// context errors) into typed [Error] values for consistent rendering.
package errors

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"net"
	"net/http"
)

// Code is dashctl's documented exit-code enum.
type Code int

// Exit-code contract — see specs/LLD/dashctl-lld.md §10.
const (
	CodeOK                 Code = 0
	CodeGeneric            Code = 1
	CodeUsage              Code = 2
	CodeNotFound           Code = 3
	CodeConflict           Code = 4
	CodeInvalidArgument    Code = 5
	CodePermissionDenied   Code = 6
	CodeUnavailable        Code = 7
	CodeTimeout            Code = 8
	CodeUnimplemented      Code = 9
	CodeInternal           Code = 10
	CodeCanceled           Code = 130 // POSIX SIGINT convention
)

// Error is the canonical dashctl error. It carries:
//   - Code:       stable exit code
//   - Reason:     human-readable message for stderr
//   - ServerCode: gRPC status / HTTP status text when available
//   - TxnID:      dashd Ack.TxnID for cross-correlation
//   - Hint:       optional next-step suggestion
//   - Wrap:       underlying cause (for errors.Is / errors.As)
type Error struct {
	Code       Code
	Reason     string
	ServerCode string
	TxnID      string
	Hint       string
	Wrap       error
}

// New builds an Error with the given code and reason.
func New(code Code, reason string) *Error {
	return &Error{Code: code, Reason: reason}
}

// Newf is the printf variant of [New].
func Newf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Reason: fmt.Sprintf(format, args...)}
}

// Wrap returns an Error wrapping cause; if cause is already an *Error it is
// returned as-is (no double-wrapping).
func Wrap(code Code, reason string, cause error) *Error {
	if cause == nil {
		return New(code, reason)
	}
	if e, ok := cause.(*Error); ok {
		return e
	}
	return &Error{Code: code, Reason: reason, Wrap: cause}
}

// Error satisfies the error interface.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Wrap != nil && e.Reason == "" {
		return e.Wrap.Error()
	}
	if e.Wrap != nil {
		return e.Reason + ": " + e.Wrap.Error()
	}
	return e.Reason
}

// Unwrap exposes the cause to errors.Is / errors.As.
func (e *Error) Unwrap() error { return e.Wrap }

// ExitCode returns the documented exit code (0 if nil).
func (e *Error) ExitCode() int {
	if e == nil {
		return 0
	}
	return int(e.Code)
}

// WithServerCode returns a copy carrying a server-side code string.
func (e *Error) WithServerCode(code string) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	cp.ServerCode = code
	return &cp
}

// WithTxnID returns a copy carrying a dashd transaction id.
func (e *Error) WithTxnID(txn string) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	cp.TxnID = txn
	return &cp
}

// WithHint returns a copy carrying an operator hint.
func (e *Error) WithHint(hint string) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	cp.Hint = hint
	return &cp
}

// ExitCodeOf inspects err and returns the documented exit code. nil → 0,
// *Error → e.ExitCode(), anything else → CodeGeneric.
func ExitCodeOf(err error) int {
	if err == nil {
		return int(CodeOK)
	}
	var e *Error
	if stderrors.As(err, &e) {
		return e.ExitCode()
	}
	return int(CodeGeneric)
}

// FromHTTPStatus maps an HTTP status code to a typed Error. The reason
// argument is a short human description (often the response body's "error"
// field). It is safe to pass an empty reason; a default will be used.
func FromHTTPStatus(status int, reason string) *Error {
	code, serverCode, hint := classifyHTTP(status)
	if reason == "" {
		reason = http.StatusText(status)
		if reason == "" {
			reason = fmt.Sprintf("HTTP %d", status)
		}
	}
	e := &Error{Code: code, Reason: reason, ServerCode: serverCode, Hint: hint}
	return e
}

func classifyHTTP(status int) (Code, string, string) {
	switch status {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent:
		return CodeOK, "OK", ""
	case http.StatusBadRequest:
		return CodeInvalidArgument, "INVALID_ARGUMENT", ""
	case http.StatusUnauthorized:
		return CodePermissionDenied, "UNAUTHENTICATED", "set --token or DASHCTL_TOKEN"
	case http.StatusForbidden:
		return CodePermissionDenied, "PERMISSION_DENIED", "your role lacks this verb"
	case http.StatusNotFound:
		return CodeNotFound, "NOT_FOUND", ""
	case http.StatusConflict:
		return CodeConflict, "FAILED_PRECONDITION", "re-fetch and retry with the latest generation"
	case http.StatusPreconditionFailed:
		return CodeConflict, "FAILED_PRECONDITION", ""
	case http.StatusTooManyRequests:
		return CodeUnavailable, "RESOURCE_EXHAUSTED", "back off and retry"
	case http.StatusGatewayTimeout:
		return CodeTimeout, "DEADLINE_EXCEEDED", ""
	case http.StatusServiceUnavailable:
		return CodeUnavailable, "UNAVAILABLE", "dashd is down or not leader"
	case http.StatusNotImplemented:
		return CodeUnimplemented, "UNIMPLEMENTED", "the dashd phase serving this RPC is not yet deployed"
	case http.StatusInternalServerError:
		return CodeInternal, "INTERNAL", ""
	default:
		switch {
		case status >= 200 && status < 300:
			return CodeOK, "OK", ""
		case status >= 400 && status < 500:
			return CodeInvalidArgument, "INVALID_ARGUMENT", ""
		case status >= 500:
			return CodeInternal, "INTERNAL", ""
		}
		return CodeGeneric, "", ""
	}
}

// Classify converts an arbitrary transport / runtime error into an *Error.
// Mapping rules:
//   - *Error                       → returned as-is
//   - context.Canceled             → CodeCanceled
//   - context.DeadlineExceeded     → CodeTimeout
//   - net.Error (timeout)          → CodeTimeout
//   - net.Error / io.EOF           → CodeUnavailable
//   - everything else              → CodeInternal
func Classify(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if stderrors.As(err, &e) {
		return e
	}
	if stderrors.Is(err, context.Canceled) {
		return New(CodeCanceled, "cancelled by user")
	}
	if stderrors.Is(err, context.DeadlineExceeded) {
		return New(CodeTimeout, "deadline exceeded")
	}
	var ne net.Error
	if stderrors.As(err, &ne) {
		if ne.Timeout() {
			return Wrap(CodeTimeout, "network timeout", err)
		}
		return Wrap(CodeUnavailable, "network error", err)
	}
	if stderrors.Is(err, io.EOF) || stderrors.Is(err, io.ErrUnexpectedEOF) {
		return Wrap(CodeUnavailable, "connection closed", err)
	}
	return Wrap(CodeInternal, err.Error(), err)
}

// Format renders the error in the stable stderr format described in
// specs/LLD/dashctl-lld.md §10.3. Safe to call on nil (returns "").
func Format(err error) string {
	if err == nil {
		return ""
	}
	e := Classify(err)
	out := "Error: " + e.Reason + "\n"
	if e.ServerCode != "" {
		out += "Code: " + e.ServerCode + "\n"
	}
	if e.TxnID != "" {
		out += "TxnId: " + e.TxnID + "\n"
	}
	if e.Hint != "" {
		out += "Hint: " + e.Hint + "\n"
	}
	return out
}

// Common sentinel errors (used by the SDK and consumed by commands via errors.Is).

// ErrNotFound indicates a missing object (used by REST backend before HTTP-status mapping in places).
var ErrNotFound = New(CodeNotFound, "not found")

// ErrUnimplemented indicates an RPC not yet supported.
var ErrUnimplemented = New(CodeUnimplemented, "not yet implemented")
