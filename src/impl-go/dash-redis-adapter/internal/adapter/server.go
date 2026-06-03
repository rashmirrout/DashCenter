// Package adapter implements dashapi.v1.DashApi backed by a Redis APP_DB.
//
// Storage model — matches what a SONiC DASH orchagent reads:
//
//   key:   "<DASH_<KIND>_TABLE>:<joined-key>"   (e.g. "DASH_VNET_TABLE:vnet-prod")
//   value: a Redis HASH with two fields:
//            "pb"   -> binary protobuf serialization of the object
//            "meta" -> JSON blob with {created_ts_ns, updated_ts_ns}
//
// Subscribe is implemented via Redis Pub/Sub on the internal channel
// "dashapi.events". Apply/Delete publish a JSON-encoded payload that the
// server fans out as dashapi.Event values to live subscribers. This avoids
// requiring keyspace notifications to be enabled on the Redis server (which
// would need `CONFIG SET notify-keyspace-events ...`).
//
// SimulatePacket is NOT supported in this backend (it has no behavioural
// pipeline state); calls return codes.Unimplemented with a hint to use the
// dash-sim binary instead.
package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	channelEvents = "dashapi.events"
	hashFieldPB   = "pb"
	hashFieldMeta = "meta"
)

// Server implements dashapi.DashApiServer over Redis.
type Server struct {
	dashapi.UnimplementedDashApiServer

	rdb    *redis.Client
	nextTx atomic.Uint64
}

// New returns a server using the given Redis client.
func New(rdb *redis.Client) *Server { return &Server{rdb: rdb} }

func (s *Server) txID() string {
	return fmt.Sprintf("tx-%d-%d", time.Now().UnixNano(), s.nextTx.Add(1))
}

func nowNs() int64 { return time.Now().UnixNano() }

// redisKey is the canonical SONiC APP_DB key.
func redisKey(info kinds.Info, key []string) string {
	return info.TableName() + ":" + strings.Join(key, ":")
}

type meta struct {
	CreatedTsNs int64 `json:"created_ts_ns"`
	UpdatedTsNs int64 `json:"updated_ts_ns"`
}

// wireEvent is what we publish to channelEvents.
type wireEvent struct {
	Type   string   `json:"type"` // CREATED|UPDATED|DELETED
	Kind   string   `json:"kind"`
	Key    []string `json:"key"`
	PB     []byte   `json:"pb"`
	TxID   string   `json:"tx_id"`
	TsNs   int64    `json:"ts_ns"`
}

// -----------------------------------------------------------------------------
// Apply / Delete / Get / List
// -----------------------------------------------------------------------------

func ack(txn string, err error) *dashapi.Ack {
	if err != nil {
		return &dashapi.Ack{TxnId: txn, Accepted: false, Error: err.Error(), ServerTsNs: nowNs()}
	}
	return &dashapi.Ack{TxnId: txn, Accepted: true, ServerTsNs: nowNs()}
}

// Apply implements DashApi.Apply.
func (s *Server) Apply(ctx context.Context, req *dashapi.ApplyRequest) (*dashapi.Ack, error) {
	tx := s.txID()
	obj := req.GetObject()
	if obj == nil {
		return ack(tx, errors.New("apply: nil object")), nil
	}
	info, err := kinds.Lookup(obj.GetKind())
	if err != nil {
		return ack(tx, err), nil
	}
	if len(obj.GetKey()) != len(info.KeyParts) {
		return ack(tx, fmt.Errorf("apply: kind %s expects %d key parts %v, got %d",
			info.Name, len(info.KeyParts), info.KeyParts, len(obj.GetKey()))), nil
	}
	for i, p := range obj.GetKey() {
		if p == "" {
			return ack(tx, fmt.Errorf("apply: kind %s key part %q empty", info.Name, info.KeyParts[i])), nil
		}
	}
	payload, err := kinds.PayloadOf(obj)
	if err != nil {
		return ack(tx, err), nil
	}
	raw, err := proto.Marshal(payload)
	if err != nil {
		return ack(tx, err), nil
	}

	rkey := redisKey(info, obj.GetKey())
	// Detect existing for CREATED vs UPDATED.
	existed, err := s.rdb.Exists(ctx, rkey).Result()
	if err != nil {
		return ack(tx, err), nil
	}
	now := nowNs()
	m := meta{CreatedTsNs: now, UpdatedTsNs: now}
	if existed == 1 {
		if existingMeta, err := s.loadMeta(ctx, rkey); err == nil && existingMeta.CreatedTsNs != 0 {
			m.CreatedTsNs = existingMeta.CreatedTsNs
		}
	}
	mBytes, _ := json.Marshal(m)
	if _, err := s.rdb.HSet(ctx, rkey, map[string]interface{}{
		hashFieldPB:   raw,
		hashFieldMeta: mBytes,
	}).Result(); err != nil {
		return ack(tx, err), nil
	}

	evType := "CREATED"
	if existed == 1 {
		evType = "UPDATED"
	}
	s.publish(ctx, wireEvent{
		Type: evType, Kind: info.Name, Key: obj.GetKey(),
		PB: raw, TxID: tx, TsNs: now,
	})
	return ack(tx, nil), nil
}

// Delete implements DashApi.Delete.
func (s *Server) Delete(ctx context.Context, req *dashapi.DeleteRequest) (*dashapi.Ack, error) {
	tx := s.txID()
	info, err := kinds.Lookup(req.GetKind())
	if err != nil {
		return ack(tx, err), nil
	}
	if len(req.GetKey()) != len(info.KeyParts) {
		return ack(tx, fmt.Errorf("delete: kind %s expects %d key parts, got %d",
			info.Name, len(info.KeyParts), len(req.GetKey()))), nil
	}
	rkey := redisKey(info, req.GetKey())
	raw, _ := s.rdb.HGet(ctx, rkey, hashFieldPB).Bytes()
	deleted, err := s.rdb.Del(ctx, rkey).Result()
	if err != nil {
		return ack(tx, err), nil
	}
	if deleted == 0 {
		return ack(tx, fmt.Errorf("not found")), nil
	}
	s.publish(ctx, wireEvent{
		Type: "DELETED", Kind: info.Name, Key: req.GetKey(),
		PB: raw, TxID: tx, TsNs: nowNs(),
	})
	return ack(tx, nil), nil
}

// Get implements DashApi.Get.
func (s *Server) Get(ctx context.Context, req *dashapi.GetRequest) (*dashapi.GetResponse, error) {
	info, err := kinds.Lookup(req.GetKind())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	rkey := redisKey(info, req.GetKey())
	raw, err := s.rdb.HGet(ctx, rkey, hashFieldPB).Bytes()
	if err == redis.Nil {
		return nil, status.Error(codes.NotFound, "not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	msg := info.NewZero()
	if err := proto.Unmarshal(raw, msg); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	out, err := kinds.WrapObject(info.Kind, req.GetKey(), msg)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &dashapi.GetResponse{Object: out, ServerTsNs: nowNs()}, nil
}

// List implements DashApi.List.
func (s *Server) List(req *dashapi.ListRequest, stream dashapi.DashApi_ListServer) error {
	info, err := kinds.Lookup(req.GetKind())
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	pattern := info.TableName() + ":*"
	if req.GetKeyPrefix() != "" {
		pattern = info.TableName() + ":" + req.GetKeyPrefix() + "*"
	}
	ctx := stream.Context()
	var cursor uint64
	count := int32(0)
	for {
		var batch []string
		var err error
		batch, cursor, err = s.rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return status.Error(codes.Internal, err.Error())
		}
		// Sort within batch for stable output.
		sortStrings(batch)
		for _, rkey := range batch {
			raw, err := s.rdb.HGet(ctx, rkey, hashFieldPB).Bytes()
			if err == redis.Nil {
				continue
			}
			if err != nil {
				return status.Error(codes.Internal, err.Error())
			}
			msg := info.NewZero()
			if err := proto.Unmarshal(raw, msg); err != nil {
				continue
			}
			// Extract key parts (everything after "TABLE:").
			suffix := strings.TrimPrefix(rkey, info.TableName()+":")
			keyParts := strings.SplitN(suffix, ":", len(info.KeyParts))
			obj, err := kinds.WrapObject(info.Kind, keyParts, msg)
			if err != nil {
				continue
			}
			if err := stream.Send(&dashapi.ListItem{Object: obj}); err != nil {
				return err
			}
			count++
			if req.GetLimit() > 0 && count >= req.GetLimit() {
				return nil
			}
		}
		if cursor == 0 {
			return nil
		}
	}
}

// -----------------------------------------------------------------------------
// Subscribe (via Redis Pub/Sub)
// -----------------------------------------------------------------------------

// Subscribe implements DashApi.Subscribe.
func (s *Server) Subscribe(req *dashapi.SubscribeRequest, stream dashapi.DashApi_SubscribeServer) error {
	ctx := stream.Context()
	wanted := map[dashapi.ObjectKind]struct{}{}
	for _, k := range req.GetKinds() {
		wanted[k] = struct{}{}
	}

	pubsub := s.rdb.Subscribe(ctx, channelEvents)
	defer pubsub.Close()

	if req.GetSnapshotFirst() {
		if err := s.streamSnapshot(stream, wanted); err != nil {
			return err
		}
	}

	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			var ev wireEvent
			if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
				continue
			}
			info, err := kinds.LookupByName(ev.Kind)
			if err != nil {
				continue
			}
			if len(wanted) > 0 {
				if _, w := wanted[info.Kind]; !w {
					continue
				}
			}
			out, err := s.eventToProto(ev, info)
			if err != nil {
				continue
			}
			if err := stream.Send(out); err != nil {
				return err
			}
		}
	}
}

func (s *Server) eventToProto(ev wireEvent, info kinds.Info) (*dashapi.Event, error) {
	msg := info.NewZero()
	if len(ev.PB) > 0 {
		if err := proto.Unmarshal(ev.PB, msg); err != nil {
			return nil, err
		}
	}
	obj, err := kinds.WrapObject(info.Kind, ev.Key, msg)
	if err != nil {
		return nil, err
	}
	t := dashapi.EventType_EVENT_TYPE_UNSPECIFIED
	switch ev.Type {
	case "CREATED":
		t = dashapi.EventType_EVENT_TYPE_CREATED
	case "UPDATED":
		t = dashapi.EventType_EVENT_TYPE_UPDATED
	case "DELETED":
		t = dashapi.EventType_EVENT_TYPE_DELETED
	case "SNAPSHOT":
		t = dashapi.EventType_EVENT_TYPE_SNAPSHOT
	}
	return &dashapi.Event{
		TxnId: ev.TxID, Type: t, Object: obj, ServerTsNs: ev.TsNs,
	}, nil
}

func (s *Server) streamSnapshot(stream dashapi.DashApi_SubscribeServer, wanted map[dashapi.ObjectKind]struct{}) error {
	ctx := stream.Context()
	for _, info := range kinds.All {
		if len(wanted) > 0 {
			if _, w := wanted[info.Kind]; !w {
				continue
			}
		}
		pattern := info.TableName() + ":*"
		var cursor uint64
		for {
			var batch []string
			var err error
			batch, cursor, err = s.rdb.Scan(ctx, cursor, pattern, 100).Result()
			if err != nil {
				return status.Error(codes.Internal, err.Error())
			}
			sortStrings(batch)
			for _, rkey := range batch {
				raw, err := s.rdb.HGet(ctx, rkey, hashFieldPB).Bytes()
				if err != nil {
					continue
				}
				suffix := strings.TrimPrefix(rkey, info.TableName()+":")
				keyParts := strings.SplitN(suffix, ":", len(info.KeyParts))
				msg := info.NewZero()
				if err := proto.Unmarshal(raw, msg); err != nil {
					continue
				}
				obj, err := kinds.WrapObject(info.Kind, keyParts, msg)
				if err != nil {
					continue
				}
				if err := stream.Send(&dashapi.Event{
					Type:       dashapi.EventType_EVENT_TYPE_SNAPSHOT,
					Object:     obj,
					ServerTsNs: nowNs(),
				}); err != nil {
					return err
				}
			}
			if cursor == 0 {
				break
			}
		}
	}
	return nil
}

func (s *Server) publish(ctx context.Context, ev wireEvent) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	s.rdb.Publish(ctx, channelEvents, b)
}

func (s *Server) loadMeta(ctx context.Context, rkey string) (meta, error) {
	var m meta
	raw, err := s.rdb.HGet(ctx, rkey, hashFieldMeta).Bytes()
	if err != nil {
		return m, err
	}
	_ = json.Unmarshal(raw, &m)
	return m, nil
}

// -----------------------------------------------------------------------------
// Counters + SimulatePacket
// -----------------------------------------------------------------------------

// GetCounters returns counters stored in Redis hash "DASH_COUNTERS:<joined-key>".
// If the hash does not exist, returns all zeros. Counters are intended to be
// written by a downstream collector; this adapter does not synthesise them.
func (s *Server) GetCounters(ctx context.Context, req *dashapi.CountersRequest) (*dashapi.CountersResponse, error) {
	key := "DASH_COUNTERS:" + strings.Join(req.GetKey(), ":")
	raw, err := s.rdb.HGetAll(ctx, key).Result()
	if err != nil && err != redis.Nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	out := map[string]int64{
		"packets_in": 0, "packets_out": 0,
		"bytes_in": 0, "bytes_out": 0, "drops": 0,
	}
	for k, v := range raw {
		var n int64
		_, _ = fmt.Sscanf(v, "%d", &n)
		out[k] = n
	}
	return &dashapi.CountersResponse{Counters: out, ServerTsNs: nowNs()}, nil
}

// SimulatePacket is not supported by the Redis backend.
func (s *Server) SimulatePacket(_ context.Context, _ *dashapi.SimulatePacketRequest) (*dashapi.SimulatePacketResponse, error) {
	return nil, status.Error(codes.Unimplemented,
		"SimulatePacket is not supported by dash-redis-adapter (Redis APP_DB has no behavioural pipeline); use the dash-sim binary instead")
}

// sortStrings is a tiny dependency-free string sorter.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
