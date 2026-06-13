// PE-G6 admin convenience endpoint: GET /admin/topology.
//
// Same shape as REST /v1/cluster/topology but on the unauthenticated
// admin port. Intended for operator scripts that already trust the
// management network — Prometheus, debugging, dashctl probes.
package admin

import (
	"net/http"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
	"google.golang.org/protobuf/encoding/protojson"
)

// SetClusterService wires the ClusterService into the admin server
// after construction. Called from main.go once the ClusterService is
// built (admin is constructed earlier in startup order).
//
// nil disables /admin/topology (404).
func (s *Server) SetClusterService(svc service.ClusterService) {
	s.clusterSvc = svc
	if s.handler != nil {
		s.handler.clusterSvc = svc
	}
}

func (h *handler) topology(w http.ResponseWriter, r *http.Request) {
	if h.clusterSvc == nil {
		writeErr(w, http.StatusServiceUnavailable, "cluster service not configured")
		return
	}
	req := &dashcenterv1.GetTopologyRequest{
		IncludeEnis: r.URL.Query().Get("include_enis") == "true",
	}
	resp, err := h.clusterSvc.GetTopology(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(resp)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "encode: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}
