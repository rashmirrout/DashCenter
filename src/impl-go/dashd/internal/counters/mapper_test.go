// mapper_test.go covers every Option B mapping decision + the
// nil-tolerance contract.

package counters

import (
	"testing"
	"time"

	dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
)

func TestMapReport_NilSource(t *testing.T) {
	got := MapReport("dpu-1", nil)
	if got == nil {
		t.Fatalf("MapReport(nil) returned nil report")
	}
	if got.GetDpuId() != "dpu-1" {
		t.Errorf("dpu_id = %q, want dpu-1", got.GetDpuId())
	}
	if got.GetSampledAt() == nil {
		t.Errorf("sampled_at must be populated even for nil source")
	}
	if got.GetVxlanDecap() != 0 || got.GetVxlanEncap() != 0 || got.GetDropAclIn() != 0 {
		t.Errorf("nil source should yield zero counters, got %+v", got)
	}
	if got.GetFlowTableSize() != 0 {
		t.Errorf("flow_table_size = %d, want 0", got.GetFlowTableSize())
	}
}

func TestMapReport_FullBucket(t *testing.T) {
	src := &dashapiv1.DpuCountersResponse{
		DeviceId:    "sim-1", // intentionally different from dpuID we pass
		SampledAtNs: time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC).UnixNano(),
		Dpu: &dashapiv1.CounterBucket{
			PacketsIn:  1247,
			PacketsOut: 2486,
			BytesIn:    87104,
			BytesOut:   174208,
			Drops:      12,
		},
		Enis: []*dashapiv1.ScopedCounters{
			{ScopeKey: "eni-001", Bucket: &dashapiv1.CounterBucket{PacketsIn: 5}},
			{ScopeKey: "eni-002", Bucket: &dashapiv1.CounterBucket{PacketsIn: 7}},
		},
		Vnets: []*dashapiv1.ScopedCounters{
			{ScopeKey: "vnet-prod", Bucket: &dashapiv1.CounterBucket{PacketsIn: 1}},
		},
	}
	got := MapReport("dpu-actual", src)
	if got.GetDpuId() != "dpu-actual" {
		t.Errorf("dpu_id = %q, want dpu-actual (caller, NOT src.device_id)", got.GetDpuId())
	}
	if got.GetVxlanDecap() != 1247 {
		t.Errorf("vxlan_decap = %d, want 1247 (packets_in)", got.GetVxlanDecap())
	}
	if got.GetVxlanEncap() != 2486 {
		t.Errorf("vxlan_encap = %d, want 2486 (packets_out)", got.GetVxlanEncap())
	}
	if got.GetDropAclIn() != 12 {
		t.Errorf("drop_acl_in = %d, want 12 (drops)", got.GetDropAclIn())
	}
	if got.GetFlowTableSize() != 3 {
		t.Errorf("flow_table_size = %d, want 3 (2 enis + 1 vnet)", got.GetFlowTableSize())
	}
	want := time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC)
	if !got.GetSampledAt().AsTime().Equal(want) {
		t.Errorf("sampled_at = %v, want %v (from sampled_at_ns)", got.GetSampledAt().AsTime(), want)
	}
}

func TestMapReport_NilDpuBucket(t *testing.T) {
	src := &dashapiv1.DpuCountersResponse{
		DeviceId:    "sim-1",
		SampledAtNs: 0, // explicitly missing → fall back to wall clock
		Dpu:         nil,
		Enis:        []*dashapiv1.ScopedCounters{{ScopeKey: "eni-001"}},
	}
	got := MapReport("dpu-z", src)
	if got.GetVxlanDecap() != 0 || got.GetVxlanEncap() != 0 || got.GetDropAclIn() != 0 {
		t.Errorf("nil dpu bucket should yield zero counters, got %+v", got)
	}
	if got.GetFlowTableSize() != 1 {
		t.Errorf("flow_table_size = %d, want 1 (1 eni scope)", got.GetFlowTableSize())
	}
	if got.GetSampledAt() == nil {
		t.Errorf("sampled_at must be populated when sampled_at_ns=0 (fallback to now)")
	}
}

func TestMapPerEni(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if got := MapPerEni(nil); got != nil {
			t.Errorf("MapPerEni(nil) = %v, want nil", got)
		}
	})
	t.Run("empty", func(t *testing.T) {
		if got := MapPerEni(&dashapiv1.DpuCountersResponse{}); got != nil {
			t.Errorf("MapPerEni(empty) = %v, want nil", got)
		}
	})
	t.Run("populated", func(t *testing.T) {
		src := &dashapiv1.DpuCountersResponse{
			SampledAtNs: time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC).UnixNano(),
			Enis: []*dashapiv1.ScopedCounters{
				{ScopeKey: "eni-001", Bucket: &dashapiv1.CounterBucket{PacketsIn: 10, PacketsOut: 20, Drops: 1}},
				{ScopeKey: "eni-002", Bucket: &dashapiv1.CounterBucket{PacketsIn: 11, PacketsOut: 22, Drops: 2}},
				{ScopeKey: "", Bucket: &dashapiv1.CounterBucket{PacketsIn: 99}}, // dropped
				nil, // dropped
			},
		}
		got := MapPerEni(src)
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2 (empty + nil entries skipped)", len(got))
		}
		r1, ok := got["eni-001"]
		if !ok {
			t.Fatalf("eni-001 missing")
		}
		if r1.GetVxlanDecap() != 10 || r1.GetVxlanEncap() != 20 || r1.GetDropAclIn() != 1 {
			t.Errorf("eni-001 = %+v, want decap=10 encap=20 drops=1", r1)
		}
		want := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
		if !r1.GetSampledAt().AsTime().Equal(want) {
			t.Errorf("eni-001 sampled_at = %v, want %v", r1.GetSampledAt().AsTime(), want)
		}
	})
}

func TestMapPerVnet(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if got := MapPerVnet(nil); got != nil {
			t.Errorf("nil source: got %v, want nil", got)
		}
	})
	t.Run("empty", func(t *testing.T) {
		if got := MapPerVnet(&dashapiv1.DpuCountersResponse{}); got != nil {
			t.Errorf("empty vnets: got %v, want nil", got)
		}
	})
	t.Run("populated", func(t *testing.T) {
		src := &dashapiv1.DpuCountersResponse{
			Vnets: []*dashapiv1.ScopedCounters{
				{ScopeKey: "vnet-prod", Bucket: &dashapiv1.CounterBucket{PacketsIn: 5}},
			},
		}
		got := MapPerVnet(src)
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		if r := got["vnet-prod"]; r == nil || r.GetVxlanDecap() != 5 {
			t.Errorf("vnet-prod = %+v, want decap=5", r)
		}
	})
}

func TestScopedBucketToReport_NilBucket(t *testing.T) {
	got := scopedBucketToReport(&dashapiv1.ScopedCounters{ScopeKey: "x"}, 0)
	if got == nil {
		t.Fatalf("nil result")
	}
	if got.GetVxlanDecap() != 0 || got.GetVxlanEncap() != 0 || got.GetDropAclIn() != 0 {
		t.Errorf("nil bucket should yield zero counters")
	}
	if got.GetSampledAt() == nil {
		t.Errorf("sampled_at must be populated even when sampledNs=0")
	}
}
