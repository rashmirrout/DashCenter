package errors

import (
	"context"
	stderrors "errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestNewAndError(t *testing.T) {
	e := New(CodeNotFound, "missing")
	if e.Error() != "missing" {
		t.Fatalf("got %q", e.Error())
	}
	if e.ExitCode() != int(CodeNotFound) {
		t.Fatalf("exit=%d", e.ExitCode())
	}
}

func TestNewfFormatsReason(t *testing.T) {
	e := Newf(CodeInvalidArgument, "bad %s=%d", "vni", 7)
	if e.Reason != "bad vni=7" {
		t.Fatalf("reason=%q", e.Reason)
	}
}

func TestErrorOnNilReceiverReturnsEmpty(t *testing.T) {
	var e *Error
	if e.Error() != "" {
		t.Fatalf("nil-error should print empty")
	}
	if e.ExitCode() != 0 {
		t.Fatalf("nil exit code should be 0")
	}
}

func TestWrapPreservesExistingError(t *testing.T) {
	inner := New(CodeConflict, "gen mismatch")
	out := Wrap(CodeInternal, "should be ignored", inner)
	if out != inner {
		t.Fatalf("wrap should return the *Error as-is")
	}
}

func TestWrapNilCauseBecomesPlain(t *testing.T) {
	out := Wrap(CodeInternal, "boom", nil)
	if out.Wrap != nil {
		t.Fatalf("nil cause must not produce wrap")
	}
}

func TestWrapStdError(t *testing.T) {
	cause := fmt.Errorf("disk full")
	out := Wrap(CodeInternal, "write failed", cause)
	if out.Error() != "write failed: disk full" {
		t.Fatalf("got %q", out.Error())
	}
	if !stderrors.Is(out, cause) {
		t.Fatalf("errors.Is should unwrap")
	}
}

func TestErrorOnlyCauseNoReason(t *testing.T) {
	cause := fmt.Errorf("dns failure")
	out := &Error{Code: CodeUnavailable, Wrap: cause}
	if out.Error() != "dns failure" {
		t.Fatalf("got %q", out.Error())
	}
}

func TestWithBuildersDoNotMutate(t *testing.T) {
	e := New(CodeConflict, "x")
	e2 := e.WithServerCode("FP").WithTxnID("tx-1").WithHint("retry")
	if e.ServerCode != "" || e.TxnID != "" || e.Hint != "" {
		t.Fatalf("original mutated")
	}
	if e2.ServerCode != "FP" || e2.TxnID != "tx-1" || e2.Hint != "retry" {
		t.Fatalf("derived not populated: %+v", e2)
	}
}

func TestWithBuildersNilSafe(t *testing.T) {
	var e *Error
	if e.WithServerCode("x") != nil || e.WithTxnID("x") != nil || e.WithHint("x") != nil {
		t.Fatalf("nil receiver must stay nil")
	}
}

func TestExitCodeOf(t *testing.T) {
	if ExitCodeOf(nil) != 0 {
		t.Fatal("nil → 0")
	}
	if ExitCodeOf(New(CodeNotFound, "")) != 3 {
		t.Fatal("typed → 3")
	}
	if ExitCodeOf(fmt.Errorf("plain")) != int(CodeGeneric) {
		t.Fatal("plain → generic")
	}
	// wrapped *Error
	if ExitCodeOf(fmt.Errorf("outer: %w", New(CodeConflict, "x"))) != int(CodeConflict) {
		t.Fatal("wrapped not detected")
	}
}

func TestFromHTTPStatusTable(t *testing.T) {
	cases := []struct {
		status int
		code   Code
		server string
	}{
		{200, CodeOK, "OK"},
		{201, CodeOK, "OK"},
		{204, CodeOK, "OK"},
		{400, CodeInvalidArgument, "INVALID_ARGUMENT"},
		{401, CodePermissionDenied, "UNAUTHENTICATED"},
		{403, CodePermissionDenied, "PERMISSION_DENIED"},
		{404, CodeNotFound, "NOT_FOUND"},
		{409, CodeConflict, "FAILED_PRECONDITION"},
		{412, CodeConflict, "FAILED_PRECONDITION"},
		{429, CodeUnavailable, "RESOURCE_EXHAUSTED"},
		{500, CodeInternal, "INTERNAL"},
		{501, CodeUnimplemented, "UNIMPLEMENTED"},
		{503, CodeUnavailable, "UNAVAILABLE"},
		{504, CodeTimeout, "DEADLINE_EXCEEDED"},
		{418, CodeInvalidArgument, "INVALID_ARGUMENT"}, // generic 4xx
		{599, CodeInternal, "INTERNAL"},                 // generic 5xx
		{102, CodeGeneric, ""},                          // 1xx oddball
	}
	for _, c := range cases {
		e := FromHTTPStatus(c.status, "")
		if e.Code != c.code {
			t.Errorf("status %d: code %v want %v", c.status, e.Code, c.code)
		}
		if e.ServerCode != c.server {
			t.Errorf("status %d: server %q want %q", c.status, e.ServerCode, c.server)
		}
	}
}

func TestFromHTTPStatusDefaultReason(t *testing.T) {
	e := FromHTTPStatus(404, "")
	if e.Reason != http.StatusText(404) {
		t.Fatalf("default reason wrong: %q", e.Reason)
	}
	e = FromHTTPStatus(404, "no such vnet")
	if e.Reason != "no such vnet" {
		t.Fatalf("custom reason wrong")
	}
	e = FromHTTPStatus(999, "")
	if e.Reason == "" {
		t.Fatalf("must synthesise reason for unknown status")
	}
}

type timeoutErr struct{ msg string }

func (t timeoutErr) Error() string   { return t.msg }
func (t timeoutErr) Timeout() bool   { return true }
func (t timeoutErr) Temporary() bool { return true }

type netErr struct{ msg string }

func (n netErr) Error() string   { return n.msg }
func (n netErr) Timeout() bool   { return false }
func (n netErr) Temporary() bool { return false }

func TestClassify(t *testing.T) {
	if Classify(nil) != nil {
		t.Fatal("nil → nil")
	}
	if e := Classify(New(CodeConflict, "x")); e.Code != CodeConflict {
		t.Fatal("typed pass-through")
	}
	if e := Classify(context.Canceled); e.Code != CodeCanceled {
		t.Fatal("context.Canceled")
	}
	if e := Classify(context.DeadlineExceeded); e.Code != CodeTimeout {
		t.Fatal("deadline exceeded")
	}
	if e := Classify(timeoutErr{msg: "boom"}); e.Code != CodeTimeout {
		t.Fatal("net timeout")
	}
	if e := Classify(netErr{msg: "no host"}); e.Code != CodeUnavailable {
		t.Fatal("net error")
	}
	if e := Classify(io.EOF); e.Code != CodeUnavailable {
		t.Fatal("EOF")
	}
	if e := Classify(io.ErrUnexpectedEOF); e.Code != CodeUnavailable {
		t.Fatal("unexpected EOF")
	}
	if e := Classify(fmt.Errorf("mystery")); e.Code != CodeInternal {
		t.Fatal("generic → internal")
	}
}

func TestClassifyRespectsNetOpError(t *testing.T) {
	// Wrap a non-timeout net.OpError to make sure the As path triggers.
	op := &net.OpError{Op: "dial", Err: fmt.Errorf("refused")}
	e := Classify(op)
	if e.Code != CodeUnavailable {
		t.Fatalf("OpError → unavailable, got %v", e.Code)
	}
}

func TestFormat(t *testing.T) {
	if Format(nil) != "" {
		t.Fatal("nil → empty")
	}
	e := New(CodeConflict, "gen mismatch").WithServerCode("FAILED_PRECONDITION").WithTxnID("tx-42").WithHint("retry")
	out := Format(e)
	for _, sub := range []string{"Error: gen mismatch", "Code: FAILED_PRECONDITION", "TxnId: tx-42", "Hint: retry"} {
		if !contains(out, sub) {
			t.Errorf("missing %q in %s", sub, out)
		}
	}
}

func TestFormatPlainErrorBecomesInternal(t *testing.T) {
	out := Format(fmt.Errorf("eek"))
	if !contains(out, "Error: eek") {
		t.Fatal("missing reason")
	}
}

func TestSentinels(t *testing.T) {
	if ErrNotFound.Code != CodeNotFound {
		t.Fatal()
	}
	if ErrUnimplemented.Code != CodeUnimplemented {
		t.Fatal()
	}
}

func TestUnwrapChain(t *testing.T) {
	root := fmt.Errorf("io err")
	mid := Wrap(CodeUnavailable, "bridge", root)
	outer := fmt.Errorf("apply: %w", mid)
	var got *Error
	if !stderrors.As(outer, &got) {
		t.Fatal("As failed")
	}
	if got.Code != CodeUnavailable {
		t.Fatal()
	}
	if !stderrors.Is(outer, root) {
		t.Fatal("root not reachable")
	}
}

// Helper: simple substring (avoids strings.Contains import for one use).
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Smoke: ensure Error is goroutine-safe (immutable after construction).
func TestErrorImmutableAfterBuilders(t *testing.T) {
	start := time.Now()
	defer func() {
		if time.Since(start) > time.Second {
			t.Fatal("test too slow")
		}
	}()
	base := New(CodeConflict, "x")
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			_ = base.WithTxnID("a")
			_ = base.WithServerCode("b")
		}()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	if base.TxnID != "" || base.ServerCode != "" {
		t.Fatal("base mutated under concurrent builders")
	}
}
