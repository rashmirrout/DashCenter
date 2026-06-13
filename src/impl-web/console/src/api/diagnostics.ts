import { api } from "./client";
import { API_BASE } from "@/lib/constants";
import type {
  TraceFlowRequest,
  TraceFlowResponse,
  ExplainMatchRequest,
  ExplainMatchResponse,
  AclHitStatsRequest,
  AclHitStatsResponse,
  ExplainDriftRequest,
  ExplainDriftResponse,
  TriggerResimRequest,
  TriggerResimResponse,
} from "./types";

const D = `${API_BASE.REST}/diagnostics`;

/**
 * PE-1 Diagnostics API client.
 *
 * All 5 endpoints are POST and proxied through dashw BFF
 * via the `/api/v1/*` wildcard → dashd REST.
 */
export const diagnosticsApi = {
  /** Simulate a packet through the full ACL→Route→Mapping pipeline. */
  traceFlow: (req: TraceFlowRequest) =>
    api.post<TraceFlowResponse>(`${D}/trace-flow`, req),

  /** Walk every candidate rule/route/mapping and explain match/non-match. */
  explainMatch: (req: ExplainMatchRequest) =>
    api.post<ExplainMatchResponse>(`${D}/explain-match`, req),

  /** Get ACL rule hit counters, optionally filtering to zero-hit rules only. */
  aclHitStats: (req: AclHitStatsRequest) =>
    api.post<AclHitStatsResponse>(`${D}/acl-hit-stats`, req),

  /** Get remediation suggestions for a specific drift item. */
  explainDrift: (req: ExplainDriftRequest) =>
    api.post<ExplainDriftResponse>(`${D}/explain-drift`, req),

  /** Trigger re-evaluation of active flows on named DPUs/ENIs. */
  triggerResimulation: (req: TriggerResimRequest) =>
    api.post<TriggerResimResponse>(`${D}/trigger-resimulation`, req),
};