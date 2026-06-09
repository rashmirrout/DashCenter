package client

import (
	"context"
	"testing"
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/config"
)

func TestRegisterAndDial(t *testing.T) {
	// Reset factories for hermetic test.
	old := factories
	factories = map[config.Transport]Factory{}
	defer func() { factories = old }()

	dialed := false
	Register("test", func(ctx context.Context, rc *config.ResolvedConfig) (Client, error) {
		dialed = true
		return nil, nil
	})
	if _, err := Dial(context.Background(), &config.ResolvedConfig{Transport: "test"}); err != nil {
		t.Fatal(err)
	}
	if !dialed {
		t.Fatal("factory not invoked")
	}
}

func TestDialNilConfig(t *testing.T) {
	if _, err := Dial(context.Background(), nil); err == nil {
		t.Fatal()
	}
}

func TestDialUnknownTransport(t *testing.T) {
	old := factories
	factories = map[config.Transport]Factory{}
	defer func() { factories = old }()
	if _, err := Dial(context.Background(), &config.ResolvedConfig{Transport: "ghost"}); err == nil {
		t.Fatal()
	}
}

func TestNewContextZeroIsCancellable(t *testing.T) {
	ctx, cancel := newContext(context.Background(), 0)
	defer cancel()
	if ctx == nil {
		t.Fatal()
	}
	cancel()
	if ctx.Err() == nil {
		t.Fatal("expected canceled")
	}
}

func TestNewContextWithDeadline(t *testing.T) {
	ctx, cancel := newContext(context.Background(), 10*time.Millisecond)
	defer cancel()
	dl, ok := ctx.Deadline()
	if !ok || dl.Before(time.Now()) {
		t.Fatal("deadline expected")
	}
}

func TestClientErrorMethods(t *testing.T) {
	e := errInvalidArgument("x")
	if e.Error() != "x" {
		t.Fatal()
	}
	if ce, ok := e.(*clientError); !ok || ce.Code() != 5 {
		t.Fatal()
	}
	if errUnimplemented("y").(*clientError).Code() != 9 {
		t.Fatal()
	}
}
