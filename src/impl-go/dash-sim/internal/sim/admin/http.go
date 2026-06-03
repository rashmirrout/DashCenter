// Package admin is the human/operator HTTP API exposed alongside the gRPC
// service. Endpoints are intentionally simple JSON in/out for quick curl
// debugging:
//
//	GET    /admin/health     -> { status, subscribers, dropped_events, sizes }
//	GET    /admin/dump       -> full model.Snapshot JSON (proto via protojson)
//	POST   /admin/reset      -> clears all model state
//	GET    /admin/faults     -> [] of faults.Spec
//	POST   /admin/faults     -> adds one fault spec (body = faults.Spec)
//	DELETE /admin/faults     -> clears all faults
//	POST   /admin/scenario   -> {"path":"/path/to/scenario.yaml"} loads scenario
package admin

import (
	"encoding/json"
	"net/http"

	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/counters"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/events"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/faults"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/model"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/scenarios"
)

// Handler bundles every dependency needed by the admin routes.
type Handler struct {
	Store    *model.Store
	Bus      *events.Bus
	Faults   *faults.Injector
	Counters *counters.Registry
	DeviceID string
}

// Mux returns a ServeMux pre-registered with every admin route.
func (h *Handler) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/health", h.health)
	mux.HandleFunc("/admin/dump", h.dump)
	mux.HandleFunc("/admin/reset", h.reset)
	mux.HandleFunc("/admin/faults", h.faultsHandler)
	mux.HandleFunc("/admin/scenario", h.scenario)
	mux.HandleFunc("/admin/counters", h.countersHandler)
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

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
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
			"GET    /admin/counters?id=<obj_id>",
		},
	})
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":          "ok",
		"device_id":       h.DeviceID,
		"subscribers":     h.Bus.SubscriberCount(),
		"dropped_events":  h.Bus.Dropped(),
		"sizes":           h.Store.Len(),
	})
}

func (h *Handler) dump(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	writeJSON(w, http.StatusOK, h.Store.Snapshot())
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
	id := r.URL.Query().Get("id")
	if id == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ids": h.Counters.IDs(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":       id,
		"counters": h.Counters.Snapshot(id),
	})
}
