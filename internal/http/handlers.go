package httpserver

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/masoudx/monitoring24/internal/alerts"
	"github.com/masoudx/monitoring24/internal/metrics"
	"github.com/masoudx/monitoring24/internal/security"
	"github.com/masoudx/monitoring24/internal/services"
	"github.com/masoudx/monitoring24/internal/storage"
	"github.com/masoudx/monitoring24/internal/tunnel"
	"github.com/masoudx/monitoring24/internal/urlcheck"
)

// LatestData is the in-memory cache updated by the collector goroutine.
type LatestData struct {
	System   *metrics.SystemSnapshot
	App      *metrics.AppSnapshot
	Network  *metrics.NetworkSnapshot
	Security *security.Snapshot
	Tunnel   *tunnel.Status
	Services *services.Snapshot
}

// Handler holds all dependencies for HTTP handlers.
type Handler struct {
	db      *storage.DB
	checker *urlcheck.Checker
	engine  *alerts.Engine

	mu     sync.RWMutex
	latest LatestData
}

func NewHandler(db *storage.DB, checker *urlcheck.Checker, engine *alerts.Engine) *Handler {
	return &Handler{db: db, checker: checker, engine: engine}
}

// UpdateLatest replaces the cached snapshot data (called from collector goroutine).
func (h *Handler) UpdateLatest(d LatestData) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d.System != nil {
		h.latest.System = d.System
	}
	if d.App != nil {
		h.latest.App = d.App
	}
	if d.Network != nil {
		h.latest.Network = d.Network
	}
	if d.Security != nil {
		h.latest.Security = d.Security
	}
	if d.Tunnel != nil {
		h.latest.Tunnel = d.Tunnel
	}
	if d.Services != nil {
		h.latest.Services = d.Services
	}
}

func (h *Handler) getLatest() LatestData {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.latest
}

// ── Helper ────────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// ── /api/metrics ──────────────────────────────────────────────────────────────

func (h *Handler) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	d := h.getLatest()
	writeJSON(w, http.StatusOK, map[string]any{
		"system":  d.System,
		"app":     d.App,
		"network": d.Network,
	})
}

// ── /api/alerts ──────────────────────────────────────────────────────────────

func (h *Handler) HandleAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	active, err := h.db.ActiveAlerts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	recent, err := h.db.RecentAlerts(50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if active == nil {
		active = []storage.Alert{}
	}
	if recent == nil {
		recent = []storage.Alert{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"active": active,
		"recent": recent,
	})
}

func (h *Handler) HandleAcknowledgeAlert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.db.AcknowledgeAlert(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ── /api/url-checks ──────────────────────────────────────────────────────────

func (h *Handler) HandleURLChecks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listURLChecks(w, r)
	case http.MethodPost:
		h.createURLCheck(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) listURLChecks(w http.ResponseWriter, r *http.Request) {
	summaries := h.checker.Summaries()
	type item struct {
		storage.URLCheck
		LastResult *urlcheck.Result `json:"last_result"`
		UptimePct  float64          `json:"uptime_pct_24h"`
	}
	out := make([]item, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, item{
			URLCheck:   s.Check,
			LastResult: s.LastResult,
			UptimePct:  s.UptimePct,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) createURLCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL             string `json:"url"`
		Label           string `json:"label"`
		IntervalSeconds int    `json:"interval_seconds"`
		TimeoutSeconds  int    `json:"timeout_seconds"`
		Enabled         *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if req.IntervalSeconds <= 0 {
		req.IntervalSeconds = 60
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 10
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	ch := storage.URLCheck{
		URL:             req.URL,
		Label:           req.Label,
		IntervalSeconds: req.IntervalSeconds,
		TimeoutSeconds:  req.TimeoutSeconds,
		Enabled:         enabled,
		CreatedAt:       time.Now(),
	}
	created, err := h.checker.Add(r.Context(), ch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) HandleURLCheckByID(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s, ok := h.checker.GetSummary(id)
		if !ok {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, s)
	case http.MethodPut:
		h.updateURLCheck(w, r, id)
	case http.MethodDelete:
		if err := h.checker.Remove(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) updateURLCheck(w http.ResponseWriter, r *http.Request, id int64) {
	existing, err := h.db.GetURLCheck(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var req struct {
		URL             *string `json:"url"`
		Label           *string `json:"label"`
		IntervalSeconds *int    `json:"interval_seconds"`
		TimeoutSeconds  *int    `json:"timeout_seconds"`
		Enabled         *bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.URL != nil {
		existing.URL = *req.URL
	}
	if req.Label != nil {
		existing.Label = *req.Label
	}
	if req.IntervalSeconds != nil {
		existing.IntervalSeconds = *req.IntervalSeconds
	}
	if req.TimeoutSeconds != nil {
		existing.TimeoutSeconds = *req.TimeoutSeconds
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if err := h.checker.Update(r.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (h *Handler) HandleURLHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	history, err := h.db.URLResultHistory(id, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if history == nil {
		history = []storage.URLResult{}
	}
	writeJSON(w, http.StatusOK, history)
}

// ── /api/thresholds ──────────────────────────────────────────────────────────

func (h *Handler) HandleThresholds(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, h.engine.GetThresholds())
	case http.MethodPut:
		var req map[string]float64
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		for k, v := range req {
			if err := h.engine.UpdateThreshold(r.Context(), k, v); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		writeJSON(w, http.StatusOK, h.engine.GetThresholds())
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ── /api/services ─────────────────────────────────────────────────────────────

func (h *Handler) HandleServices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	d := h.getLatest()
	writeJSON(w, http.StatusOK, d.Services)
}

// ── /api/security ─────────────────────────────────────────────────────────────

func (h *Handler) HandleSecurity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	d := h.getLatest()
	writeJSON(w, http.StatusOK, d.Security)
}

// ── /api/tunnel ───────────────────────────────────────────────────────────────

func (h *Handler) HandleTunnel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	d := h.getLatest()
	writeJSON(w, http.StatusOK, d.Tunnel)
}
