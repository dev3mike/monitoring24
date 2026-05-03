package tests

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/masoudx/monitoring24/internal/alerts"
	"github.com/masoudx/monitoring24/internal/config"
	httpserver "github.com/masoudx/monitoring24/internal/http"
	"github.com/masoudx/monitoring24/internal/sse"
	"github.com/masoudx/monitoring24/internal/storage"
	"github.com/masoudx/monitoring24/internal/urlcheck"
	"golang.org/x/crypto/bcrypt"
)

func testAssets() fs.FS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><html></html>")},
	}
}

func newTestHandler(t *testing.T) (*storage.DB, *httpserver.Handler, *alerts.Engine, *urlcheck.Checker, context.Context, context.CancelFunc) {
	t.Helper()
	db := openTestDB(t)
	eng := alerts.NewEngine(db)
	ctx, cancel := context.WithCancel(context.Background())
	if err := eng.LoadThresholds(ctx); err != nil {
		t.Fatal(err)
	}
	chk := urlcheck.NewChecker(db)
	h := httpserver.NewHandler(db, chk, eng)
	return db, h, eng, chk, ctx, cancel
}

func TestHTTP_MetricsGET(t *testing.T) {
	// given
	_, h, _, _, _, cancel := newTestHandler(t)
	defer cancel()
	mux := http.NewServeMux()
	cfg := &config.Config{}
	httpserver.SetupRoutes(mux, h, sse.NewBroker(), cfg, testAssets())
	h.UpdateLatest(httpserver.LatestData{})

	// when
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	mux.ServeHTTP(rec, req)

	// then
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestHTTP_ThresholdsPUTGET(t *testing.T) {
	// given
	_, h, _, _, _, cancel := newTestHandler(t)
	defer cancel()
	mux := http.NewServeMux()
	cfg := &config.Config{}
	httpserver.SetupRoutes(mux, h, sse.NewBroker(), cfg, testAssets())

	// when
	put := httptest.NewRecorder()
	reqPut := httptest.NewRequest(http.MethodPut, "/api/thresholds", strings.NewReader(`{"cpu_pct":77}`))
	reqPut.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(put, reqPut)
	get := httptest.NewRecorder()
	reqGet := httptest.NewRequest(http.MethodGet, "/api/thresholds", nil)
	mux.ServeHTTP(get, reqGet)

	// then
	if put.Code != http.StatusOK {
		t.Fatalf("put status %d: %s", put.Code, put.Body.String())
	}
	var m map[string]float64
	if err := json.NewDecoder(get.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	if m["cpu_pct"] != 77 {
		t.Fatalf("expected cpu_pct 77, got %v", m["cpu_pct"])
	}
}

func TestHTTP_BasicAuthRejectsAndAccepts(t *testing.T) {
	// given
	_, h, _, _, _, cancel := newTestHandler(t)
	defer cancel()
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		BasicAuthUser: "admin",
		BasicAuthHash: hash,
	}
	mux := http.NewServeMux()
	httpserver.SetupRoutes(mux, h, sse.NewBroker(), cfg, testAssets())

	// when
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	mux.ServeHTTP(rec, req)

	// then
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", rec.Code)
	}

	// when
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	req2.SetBasicAuth("admin", "secret")
	mux.ServeHTTP(rec2, req2)

	// then
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid auth, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

func TestHTTP_URLChecksPOSTAndGET(t *testing.T) {
	// given
	_, h, _, _, ctx, cancel := newTestHandler(t)
	defer cancel()
	mux := http.NewServeMux()
	cfg := &config.Config{}
	httpserver.SetupRoutes(mux, h, sse.NewBroker(), cfg, testAssets())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	body := `{"url":` + jsonQuote(srv.URL) + `,"label":"x","interval_seconds":120,"timeout_seconds":5,"enabled":true}`

	// when
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/url-checks", strings.NewReader(body))
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)

	// then
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d: %s", rec.Code, rec.Body.String())
	}

	// when
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/url-checks", nil)
	mux.ServeHTTP(rec2, req2)

	// then
	if rec2.Code != http.StatusOK {
		t.Fatalf("list status %d", rec2.Code)
	}
	var list []map[string]any
	if err := json.NewDecoder(rec2.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 check, got %d", len(list))
	}
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestHTTP_MethodNotAllowed(t *testing.T) {
	// given
	_, h, _, _, _, cancel := newTestHandler(t)
	defer cancel()
	mux := http.NewServeMux()
	cfg := &config.Config{}
	httpserver.SetupRoutes(mux, h, sse.NewBroker(), cfg, testAssets())

	// when
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/thresholds", nil)
	mux.ServeHTTP(rec, req)

	// then
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHTTP_SSEStreamSendsEvent(t *testing.T) {
	// given
	broker := sse.NewBroker()
	done := make(chan struct{})
	go broker.Run(done)
	defer close(done)
	_, h, _, _, _, cancel := newTestHandler(t)
	defer cancel()
	mux := http.NewServeMux()
	cfg := &config.Config{}
	httpserver.SetupRoutes(mux, h, broker, cfg, testAssets())

	// when
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	ctx, cancel2 := context.WithCancel(context.Background())
	req = req.WithContext(ctx)
	go func() {
		time.Sleep(80 * time.Millisecond)
		broker.BroadcastJSON("metrics", map[string]int{"ok": 1})
		time.Sleep(80 * time.Millisecond)
		cancel2()
	}()
	mux.ServeHTTP(rec, req)

	// then
	body := rec.Body.String()
	if !strings.Contains(body, "event: metrics") || !strings.Contains(body, `"ok":1`) {
		t.Fatalf("unexpected SSE body: %q", body)
	}
}

func TestHTTP_StaticRootServesIndex(t *testing.T) {
	// given
	_, h, _, _, _, cancel := newTestHandler(t)
	defer cancel()
	mux := http.NewServeMux()
	cfg := &config.Config{}
	httpserver.SetupRoutes(mux, h, sse.NewBroker(), cfg, testAssets())

	// when
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	mux.ServeHTTP(rec, req)

	// then
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	slurp, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(slurp), "html") {
		t.Fatalf("unexpected body %q", slurp)
	}
}
