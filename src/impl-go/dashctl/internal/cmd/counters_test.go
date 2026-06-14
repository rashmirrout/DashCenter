// counters_test.go — UT for the `dashctl counters` subcommand.
//
// Drives the REST backend via fakeClient. The gRPC backend has its
// own bufconn-based tests in pkg/client/grpc/counters_test.go; here we
// cover flag parsing, output rendering, and error surfaces.

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
)

func runCounters(t *testing.T, fc *fakeClient, args ...string) (string, string, error) {
	t.Helper()
	a, out, errb := testApp(t, fc)
	// Capture stdout (counters cmd writes via fmt.Printf to os.Stdout).
	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	code := runArgs(a, append([]string{"counters"}, args...)...)
	_ = w.Close()
	stdoutBytes, _ := io.ReadAll(r)

	combined := stdoutBytes
	if out.Len() > 0 {
		combined = append(combined, out.Bytes()...)
	}
	var err error
	if code != 0 {
		// Surface the Err buffer as a synthetic error so tests can
		// assert on it without inspecting exit codes.
		err = errFromExit(code, errb.String())
	}
	return string(combined), errb.String(), err
}

// errFromExit is a tiny helper that wraps a non-zero exit with the
// captured stderr so tests reading err.Error() see the message.
func errFromExit(code int, msg string) error {
	return cmdExitError{code: code, msg: strings.TrimSpace(msg)}
}

type cmdExitError struct {
	code int
	msg  string
}

func (e cmdExitError) Error() string {
	if e.msg == "" {
		return "exit " + fmtInt(e.code)
	}
	return e.msg
}

func fmtInt(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	out := []byte{}
	for i > 0 {
		out = append([]byte{byte('0' + i%10)}, out...)
		i /= 10
	}
	if neg {
		out = append([]byte{'-'}, out...)
	}
	return string(out)
}

func TestCounters_Snapshot_TableDefault(t *testing.T) {
	// (os.Stdout shared) t.Parallel() disabled
	fc := &fakeClient{
		getCountersFn: func(ctx context.Context, ids []string) (*client.CountersSnapshot, error) {
			return &client.CountersSnapshot{Reports: []*client.CounterReport{
				{DpuId: "dpu-a", VxlanDecap: "42"},
				{DpuId: "dpu-b", VxlanDecap: "99"},
			}}, nil
		},
	}
	stdout, _, err := runCounters(t, fc)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(stdout, "dpu-a") || !strings.Contains(stdout, "dpu-b") {
		t.Errorf("missing DPU rows in:\n%s", stdout)
	}
	if !strings.Contains(stdout, "42") || !strings.Contains(stdout, "99") {
		t.Errorf("missing counter values in:\n%s", stdout)
	}
}

func TestCounters_Snapshot_JSON(t *testing.T) {
	// (os.Stdout shared) t.Parallel() disabled
	fc := &fakeClient{
		getCountersFn: func(ctx context.Context, ids []string) (*client.CountersSnapshot, error) {
			return &client.CountersSnapshot{Reports: []*client.CounterReport{
				{DpuId: "dpu-a", VxlanDecap: "5"},
			}}, nil
		},
	}
	stdout, _, err := runCounters(t, fc, "--json")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var snap client.CountersSnapshot
	if err := json.Unmarshal([]byte(stdout), &snap); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if len(snap.Reports) != 1 || snap.Reports[0].DpuId != "dpu-a" {
		t.Errorf("decoded snapshot wrong: %+v", snap)
	}
}

func TestCounters_Snapshot_CSV(t *testing.T) {
	// (os.Stdout shared) t.Parallel() disabled
	fc := &fakeClient{
		getCountersFn: func(ctx context.Context, ids []string) (*client.CountersSnapshot, error) {
			return &client.CountersSnapshot{Reports: []*client.CounterReport{
				{DpuId: "dpu-a", VxlanDecap: "5", VxlanEncap: "6"},
			}}, nil
		},
	}
	stdout, _, err := runCounters(t, fc, "--csv")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(stdout, "dpu_id,sampled_at") {
		t.Errorf("missing CSV header in:\n%s", stdout)
	}
	if !strings.Contains(stdout, "dpu-a,") || !strings.Contains(stdout, ",5,6,") {
		t.Errorf("missing CSV row in:\n%s", stdout)
	}
}

func TestCounters_Snapshot_PassesDpuFilter(t *testing.T) {
	// (os.Stdout shared) t.Parallel() disabled
	var sawIds []string
	fc := &fakeClient{
		getCountersFn: func(ctx context.Context, ids []string) (*client.CountersSnapshot, error) {
			sawIds = ids
			return &client.CountersSnapshot{}, nil
		},
	}
	if _, _, err := runCounters(t, fc, "--dpu=dpu-a", "--dpu=dpu-b"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(sawIds) != 2 || sawIds[0] != "dpu-a" || sawIds[1] != "dpu-b" {
		t.Errorf("ids = %v", sawIds)
	}
}

func TestCounters_Follow_DispatchesEvents(t *testing.T) {
	// (os.Stdout shared) t.Parallel() disabled
	fc := &fakeClient{
		streamCountersFn: func(ctx context.Context, opts client.CountersWatchOptions) error {
			_ = opts.OnEvent(client.CounterEvent{Kind: "KIND_SNAPSHOT", EventID: 1, Report: &client.CounterReport{DpuId: "dpu-a", VxlanDecap: "1"}})
			_ = opts.OnEvent(client.CounterEvent{Kind: "KIND_REPORT", EventID: 2, Report: &client.CounterReport{DpuId: "dpu-a", VxlanDecap: "2"}})
			return nil
		},
	}
	stdout, _, err := runCounters(t, fc, "--follow")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(stdout, "[KIND_SNAPSHOT id=1]") {
		t.Errorf("missing snapshot line: %s", stdout)
	}
	if !strings.Contains(stdout, "[KIND_REPORT id=2]") {
		t.Errorf("missing report line: %s", stdout)
	}
}

func TestCounters_Follow_PassesLastEventID(t *testing.T) {
	// (os.Stdout shared) t.Parallel() disabled
	var sawOpts client.CountersWatchOptions
	fc := &fakeClient{
		streamCountersFn: func(ctx context.Context, opts client.CountersWatchOptions) error {
			sawOpts = opts
			return nil
		},
	}
	if _, _, err := runCounters(t, fc, "--follow", "--since-id=42"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if sawOpts.LastEventID != 42 {
		t.Errorf("LastEventID = %d, want 42", sawOpts.LastEventID)
	}
}

func TestCounters_BackendValidation(t *testing.T) {
	// (os.Stdout shared) t.Parallel() disabled
	_, _, err := runCounters(t, &fakeClient{}, "--backend=hax")
	if err == nil || !strings.Contains(err.Error(), "unsupported --backend") {
		t.Errorf("err = %v", err)
	}
}

func TestCounters_Follow_OnEventSentinelStops(t *testing.T) {
	// (os.Stdout shared) t.Parallel() disabled
	stopErr := errors.New("client stopped")
	fc := &fakeClient{
		streamCountersFn: func(ctx context.Context, opts client.CountersWatchOptions) error {
			if err := opts.OnEvent(client.CounterEvent{Kind: "KIND_SNAPSHOT"}); err != nil {
				return err
			}
			return stopErr
		},
	}
	_, errOut, err := runCounters(t, fc, "--follow")
	if err == nil {
		t.Fatalf("expected non-zero exit when stream returns error")
	}
	if !strings.Contains(errOut, "client stopped") {
		t.Errorf("stderr missing 'client stopped': %s", errOut)
	}
}

func TestGuessGrpcFromRest(t *testing.T) {
	// (os.Stdout shared) t.Parallel() disabled
	cases := map[string]string{
		"http://localhost:8443":  "localhost:9443",
		"https://dashd-1:28443":  "dashd-1:29443",
		"http://10.0.0.1:18443":  "10.0.0.1:19443",
	}
	for in, want := range cases {
		got, err := guessGrpcFromRest(in)
		if err != nil {
			t.Errorf("guess(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("guess(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGuessGrpcFromRest_Errors(t *testing.T) {
	// (os.Stdout shared) t.Parallel() disabled
	if _, err := guessGrpcFromRest("http://"); err == nil {
		t.Errorf("expected error for empty host:port")
	}
	if _, err := guessGrpcFromRest("not a url ⌘"); err == nil {
		t.Errorf("expected error for malformed url")
	}
}
