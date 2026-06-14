// Types mirroring the dashd dashcenter.v1.TopologyEvent / TopologyResponse
// wire shape. Kept hand-rolled (rather than codegen-from-proto) because
// the SPA already follows that pattern for the other API responses in
// `src/api/types.ts`.
//
// The protojson wire format uses `snake_case` field names + the
// `KIND_*` enum strings, both preserved here.

export type TopologyEventKind =
  | 'KIND_UNSPECIFIED'
  | 'KIND_SNAPSHOT'
  | 'KIND_PEER_ADDED'
  | 'KIND_PEER_REMOVED'
  | 'KIND_PEER_UPDATED'
  | 'KIND_LEADER_CHANGED'
  | 'KIND_DPU_STATE'
  | 'KIND_DPU_ADDED'
  | 'KIND_DPU_REMOVED'
  | 'KIND_KEEPALIVE'
  | 'KIND_DROPPED'
  | 'KIND_RATE_LIMITED'
  | 'KIND_RESYNC';

export interface ClusterNode {
  node_id: string;
  rest_addr?: string;
  grpc_addr?: string;
  admin_addr?: string;
  version?: string;
  build_sha?: string;
  started_at?: string;   // RFC3339
  is_leader?: boolean;
  labels?: Record<string, string>;
}

export interface ClusterInfo {
  healthy: boolean;
  leader_id?: string;
  node_count: number;
  nodes: ClusterNode[];
}

export interface EniTop {
  name: string;
  namespace?: string;
  vnet_name?: string;
  mac_address?: string;
  admin_state?: string;
}

export interface DpuTop {
  id: string;
  slot?: number;
  state: string;        // "DPU_STATE_UP" | ...
  last_seen?: string;
  eni_count: number;
  cordoned?: boolean;
  enis?: EniTop[];
}

export interface ApplianceTop {
  id: string;
  zone?: string;
  tier?: string;
  dpus: DpuTop[];
}

export interface ZoneTop {
  zone: string;
  appliance_count: number;
  dpu_count: number;
  eni_count: number;
}

export interface TopologySummaryV2 {
  total_nodes: number;
  total_appliances: number;
  total_dpus: number;
  total_enis: number;
  healthy_dpus: number;
  degraded_dpus: number;
  offline_dpus: number;
  cordoned_dpus: number;
}

export interface NamespaceObjectCounts {
  vnets?: number;
  enis?: number;
  vnet_mappings?: number;
  acl_policies?: number;
  route_policies?: number;
  ha_sets?: number;
  service_tunnels?: number;
}

export interface TopologyV2Response {
  computed_at?: string;
  cluster?: ClusterInfo;
  appliances?: ApplianceTop[];
  zones?: ZoneTop[];
  summary?: TopologySummaryV2;
  objects?: Record<string, NamespaceObjectCounts>;
}

export interface NoticePayload {
  dropped_count?: number;
  suppressed_count?: number;
  message?: string;
  current_event_id?: number;
}

export interface TopologyEvent {
  kind: TopologyEventKind;
  ts?: string;
  event_id?: number;
  snapshot?: TopologyV2Response;
  peer?: ClusterNode;
  dpu?: DpuTop;
  notice?: NoticePayload;
  old_leader_id?: string;
  new_leader_id?: string;
  /**
   * PE-G7 provenance fields stamped by dashw (the BFF) onto every
   * fan-out frame so the browser can identify the path the event
   * travelled:
   *   - `source` = upstream dashd identity (e.g. "dashd-1:9443").
   *               Present on EVERY frame including KEEPALIVE, so the
   *               operator knows which dashd produced even idle frames.
   *   - `via`    = this dashw replica's identity (hostname or
   *               --node-id flag value). Useful when N dashw replicas
   *               sit behind an LB and you need to know which one
   *               handled a session.
   */
  source?: string;
  via?: string;
}
