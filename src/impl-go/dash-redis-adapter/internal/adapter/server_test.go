package adapter_test

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	dash_eni "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/eni"
	dash_vnet "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet"
	"github.com/alicebob/miniredis/v2"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-redis-adapter/internal/adapter"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// newTestServer spins up the adapter against an in-process miniredis +
// in-memory bufconn gRPC server. Returns a connected DashApi client.
func newTestServer(t *testing.T) (dashapi.DashApiClient, func()) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	svc := adapter.New(rdb)
	gsrv := grpc.NewServer()
	dashapi.RegisterDashApiServer(gsrv, svc)

	lis := bufconn.Listen(1024 * 1024)
	go func() { _ = gsrv.Serve(lis) }()

	dialer := func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }
	conn, err := grpc.NewClient("passthrough://bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	cleanup := func() {
		_ = conn.Close()
		gsrv.Stop()
		_ = rdb.Close()
	}
	return dashapi.NewDashApiClient(conn), cleanup
}

func TestApply_Get_Delete(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	obj, err := kinds.WrapObject(dashapi.ObjectKind_OBJECT_KIND_VNET,
		[]string{"vnet-prod"},
		&dash_vnet.Vnet{Vni: 1001},
	)
	if err != nil {
		t.Fatal(err)
	}

	// Apply CREATED
	ack, err := cl.Apply(ctx, &dashapi.ApplyRequest{Object: obj})
	if err != nil || !ack.GetAccepted() {
		t.Fatalf("apply: err=%v ack=%v", err, ack)
	}
	// Get
	resp, err := cl.Get(ctx, &dashapi.GetRequest{
		Kind: dashapi.ObjectKind_OBJECT_KIND_VNET, Key: []string{"vnet-prod"},
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	gotV := resp.GetObject().GetVnet()
	if gotV == nil || gotV.GetVni() != 1001 {
		t.Errorf("vnet round-trip mismatch: %+v", gotV)
	}
	// Apply UPDATED (same key, different vni)
	updated, _ := kinds.WrapObject(dashapi.ObjectKind_OBJECT_KIND_VNET,
		[]string{"vnet-prod"}, &dash_vnet.Vnet{Vni: 1099})
	if _, err := cl.Apply(ctx, &dashapi.ApplyRequest{Object: updated}); err != nil {
		t.Fatalf("apply update: %v", err)
	}
	resp, _ = cl.Get(ctx, &dashapi.GetRequest{
		Kind: dashapi.ObjectKind_OBJECT_KIND_VNET, Key: []string{"vnet-prod"},
	})
	if resp.GetObject().GetVnet().GetVni() != 1099 {
		t.Errorf("expected updated vni=1099")
	}
	// Delete
	ack, err = cl.Delete(ctx, &dashapi.DeleteRequest{
		Kind: dashapi.ObjectKind_OBJECT_KIND_VNET, Key: []string{"vnet-prod"},
	})
	if err != nil || !ack.GetAccepted() {
		t.Fatalf("delete: err=%v ack=%v", err, ack)
	}
	// Get -> NotFound
	_, err = cl.Get(ctx, &dashapi.GetRequest{
		Kind: dashapi.ObjectKind_OBJECT_KIND_VNET, Key: []string{"vnet-prod"},
	})
	if status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound after delete, got %v", err)
	}
}

func TestList_OrderedByKey(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	for _, k := range []string{"vnet-c", "vnet-a", "vnet-b"} {
		obj, _ := kinds.WrapObject(dashapi.ObjectKind_OBJECT_KIND_VNET, []string{k}, &dash_vnet.Vnet{Vni: 1})
		if _, err := cl.Apply(ctx, &dashapi.ApplyRequest{Object: obj}); err != nil {
			t.Fatal(err)
		}
	}
	stream, err := cl.List(ctx, &dashapi.ListRequest{Kind: dashapi.ObjectKind_OBJECT_KIND_VNET})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for {
		item, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, item.GetObject().GetKey()[0])
	}
	want := []string{"vnet-a", "vnet-b", "vnet-c"}
	if len(keys) != len(want) {
		t.Fatalf("len mismatch: got %v want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("keys[%d]=%s want %s", i, keys[i], want[i])
		}
	}
}

func TestSubscribe_SnapshotAndLive(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Pre-populate.
	for _, k := range []string{"vnet-1", "vnet-2"} {
		obj, _ := kinds.WrapObject(dashapi.ObjectKind_OBJECT_KIND_VNET, []string{k}, &dash_vnet.Vnet{Vni: 1})
		if _, err := cl.Apply(ctx, &dashapi.ApplyRequest{Object: obj}); err != nil {
			t.Fatal(err)
		}
	}

	stream, err := cl.Subscribe(ctx, &dashapi.SubscribeRequest{
		Kinds:         []dashapi.ObjectKind{dashapi.ObjectKind_OBJECT_KIND_VNET},
		SnapshotFirst: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	type item struct {
		t   dashapi.EventType
		key string
	}
	got := make(chan item, 16)
	go func() {
		defer close(got)
		for {
			ev, err := stream.Recv()
			if err != nil {
				return
			}
			got <- item{ev.GetType(), ev.GetObject().GetKey()[0]}
		}
	}()

	collect := func(n int) []item {
		out := make([]item, 0, n)
		deadline := time.After(3 * time.Second)
		for len(out) < n {
			select {
			case x, ok := <-got:
				if !ok {
					return out
				}
				out = append(out, x)
			case <-deadline:
				return out
			}
		}
		return out
	}

	snap := collect(2)
	if len(snap) != 2 {
		t.Fatalf("snapshot want 2 got %d (%v)", len(snap), snap)
	}
	for _, s := range snap {
		if s.t != dashapi.EventType_EVENT_TYPE_SNAPSHOT {
			t.Errorf("snapshot type want SNAPSHOT got %s", s.t)
		}
	}

	// Live event.
	obj, _ := kinds.WrapObject(dashapi.ObjectKind_OBJECT_KIND_VNET, []string{"vnet-live"}, &dash_vnet.Vnet{Vni: 9999})
	if _, err := cl.Apply(ctx, &dashapi.ApplyRequest{Object: obj}); err != nil {
		t.Fatal(err)
	}
	live := collect(1)
	if len(live) != 1 || live[0].t != dashapi.EventType_EVENT_TYPE_CREATED || live[0].key != "vnet-live" {
		t.Errorf("live event got %+v want one CREATED vnet-live", live)
	}
}

func TestEni_RoundTrip(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	obj, _ := kinds.WrapObject(dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-001"},
		&dash_eni.Eni{
			EniId:      "11111111-1111-1111-1111-111111111111",
			MacAddress: []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
			Vnet:       "vnet-prod",
			AdminState: dash_eni.State_STATE_ENABLED,
		})
	if _, err := cl.Apply(ctx, &dashapi.ApplyRequest{Object: obj}); err != nil {
		t.Fatal(err)
	}
	resp, err := cl.Get(ctx, &dashapi.GetRequest{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ENI, Key: []string{"eni-001"},
	})
	if err != nil {
		t.Fatal(err)
	}
	eni := resp.GetObject().GetEni()
	if eni.GetAdminState() != dash_eni.State_STATE_ENABLED {
		t.Errorf("admin_state want ENABLED got %s", eni.GetAdminState())
	}
	if len(eni.GetMacAddress()) != 6 || eni.GetMacAddress()[5] != 0x55 {
		t.Errorf("mac round-trip mismatch: %x", eni.GetMacAddress())
	}
}

func TestSimulatePacket_Unimplemented(t *testing.T) {
	cl, cleanup := newTestServer(t)
	defer cleanup()
	_, err := cl.SimulatePacket(context.Background(), &dashapi.SimulatePacketRequest{
		Packet: &dashapi.Packet{Direction: dashapi.Packet_DIRECTION_OUTBOUND, Eni: "x"},
	})
	if status.Code(err) != codes.Unimplemented {
		t.Errorf("want Unimplemented got %v", err)
	}
}
