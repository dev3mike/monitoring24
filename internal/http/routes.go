package httpserver

import (
	"io/fs"
	"log"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/masoudx/monitoring24/internal/config"
	"github.com/masoudx/monitoring24/internal/sse"
)

// SetupRoutes wires all API and static routes onto mux.
func SetupRoutes(mux *http.ServeMux, h *Handler, broker *sse.Broker, cfg *config.Config, assets fs.FS) {
	wrap := func(fn http.HandlerFunc) http.HandlerFunc {
		fn = withLogging(fn)
		if cfg.AuthEnabled() {
			fn = withBasicAuth(fn, cfg)
		}
		return fn
	}

	// API routes
	mux.HandleFunc("GET /api/metrics", wrap(h.HandleMetrics))
	mux.HandleFunc("GET /api/alerts", wrap(h.HandleAlerts))
	mux.HandleFunc("POST /api/alerts/{id}/acknowledge", wrap(h.HandleAcknowledgeAlert))
	mux.HandleFunc("/api/url-checks", wrap(h.HandleURLChecks))
	mux.HandleFunc("/api/url-checks/{id}", wrap(h.HandleURLCheckByID))
	mux.HandleFunc("GET /api/url-checks/{id}/history", wrap(h.HandleURLHistory))
	mux.HandleFunc("/api/thresholds", wrap(h.HandleThresholds))
	mux.HandleFunc("GET /api/services", wrap(h.HandleServices))
	mux.HandleFunc("GET /api/security", wrap(h.HandleSecurity))
	mux.HandleFunc("GET /api/tunnel", wrap(h.HandleTunnel))

	// SSE stream
	sseHandler := wrap(broker.ServeHTTP)
	mux.HandleFunc("GET /events", sseHandler)

	// Static assets (embedded)
	mux.Handle("/", http.FileServer(http.FS(assets)))
}

func withLogging(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, code: http.StatusOK}
		next(rw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.code, time.Since(start))
	}
}

type responseWriter struct {
	http.ResponseWriter
	code int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.code = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush preserves optional streaming support (required for SSE endpoints).
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func withBasicAuth(next http.HandlerFunc, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != cfg.BasicAuthUser || bcrypt.CompareHashAndPassword(cfg.BasicAuthHash, []byte(pass)) != nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="monitoring24"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
