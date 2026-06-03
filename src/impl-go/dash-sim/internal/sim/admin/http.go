// Package admin is the operator HTTP API.
//
//   GET    /admin/health        -> {status, subscribers, dropped_events, sizes}
//   GET    /admin/dump          -> {<kind>: [{key, value}, ...], ...}
//   POST   /admin/reset         -> clear model
//   GET    /admin/faults        -> [Spec]
//   POST   /admin/faults        -> add a Spec
//   DELETE /admin/faults        -> clear all
//   POST   /admin/scenario      -> {path, reset?} loads scenario
//   GET    /admin/counters?k=a:b -> counters for joined key
//   GET    /admin/kinds         -> list of supported kind names
package admin

import (
	"encoding/json"
	"net/http"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/counters"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/events"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/faults"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/model"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/scenarios"
	"google.golang.org/protobuf/encoding/protojson"
)

// Handler bundles deps.
type Handler struct {
	Store    *model.Store
	Bus      *events.Bus
	Faults   *faults.Injector
	Counters *counters.Registry
	DeviceID string
}

// Mux returns a ServeMux with every admin route registered.
func (h *Handler) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/health", h.health)
	mux.HandleFunc("/admin/dump", h.dump)
	mux.HandleFunc("/admin/reset", h.reset)
	mux.HandleFunc("/admin/faults", h.faultsHandler)
	mux.HandleFunc("/admin/scenario", h.scenario)
	mux.HandleFunc("/admin/counters", h.countersHandler)
	mux.HandleFunc("/admin/kinds", h.kindsHandler)
	mux.HandleFunc("/", h.index)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (h *Handler) index(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"service": "dash-sim admin",
		"routes": []string{
			"GET    /admin/health",
			"GET    /admin/dump",
			"POST   /admin/reset",
			"GET    /admin/faults",
			"POST   /admin/faults",
			"DELETE /admin/faults",
			"POST   /admin/scenario",
			"GET    /admin/counters?k=<joined-key>",
			"GET    /admin/kinds",
		},
	})
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":         "ok",
		"device_id":      h.DeviceID,
		"subscribers":    h.Bus.SubscriberCount(),
		"dropped_events": h.Bus.Dropped(),
		"sizes":          h.Store.Len(),
	})
}

func (h *Handler) dump(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	marshaler := protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false}
	out := make(map[string][]map[string]interface{}, len(kinds.All))
	for _, info := range kinds.All {
		items, err := h.Store.List(info.Kind, "")
		if err != nil {
			continue
		}
		rendered := make([]map[string]interface{}, 0, len(items))
		for _, obj := range items {
			payload, _ := kinds.PayloadOf(obj)
			raw, _ := marshaler.Marshal(payload)
			var v interface{}
			_ = json.Unmarshal(raw, &v)
			rendered = append(rendered, map[string]interface{}{
				"key":   obj.GetKey(),
				"value": v,
			})
		}
		if len(rendered) > 0 {
			out[info.Name] = rendered
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) reset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	h.Store.Reset()
	writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

func (h *Handler) faultsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, h.Faults.List())
	case http.MethodDelete:
		h.Faults.Clear()
		writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
	case http.MethodPost:
		var spec faults.Spec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := h.Faults.Add(spec); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, spec)
	default:
		writeError(w, http.StatusMethodNotAllowed, "GET, POST or DELETE only")
	}
}

func (h *Handler) scenario(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var body struct {
		Path  string `json:"path"`
		Reset bool   `json:"reset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Reset {
		h.Store.Reset()
	}
	if err := scenarios.LoadFile(body.Path, h.Store); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "loaded",
		"path":   body.Path,
		"sizes":  h.Store.Len(),
	})
}

func (h *Handler) countersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	k := r.URL.Query().Get("k")
	if k == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{"keys": h.Counters.Keys()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"key":      k,
		"counters": h.Counters.Snapshot(k),
	})
}

func (h *Handler) kindsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	out := make([]map[string]interface{}, 0, len(kinds.All))
	for _, info := range kinds.All {
		out = append(out, map[string]interface{}{
			"kind":     dashapi.ObjectKind_name[int32(info.Kind)],
			"name":     info.Name,
			"key_parts": info.KeyParts,
		})
	}
	writeJSON(w, http.StatusOK, out)
}
