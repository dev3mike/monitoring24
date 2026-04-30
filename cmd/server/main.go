package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/masoudx/monitoring24/internal/alerts"
	"github.com/masoudx/monitoring24/internal/config"
	httpserver "github.com/masoudx/monitoring24/internal/http"
	"github.com/masoudx/monitoring24/internal/metrics"
	"github.com/masoudx/monitoring24/internal/security"
	"github.com/masoudx/monitoring24/internal/services"
	"github.com/masoudx/monitoring24/internal/sse"
	"github.com/masoudx/monitoring24/internal/storage"
	"github.com/masoudx/monitoring24/internal/tunnel"
	"github.com/masoudx/monitoring24/internal/urlcheck"
	webpkg "github.com/masoudx/monitoring24/web"
)

var startTime = time.Now()

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	cfg := config.ParseFlags()

	// ── Storage ──────────────────────────────────────────────────────────────
	dbPath := cfg.DataDir + "/monitor.db"
	db, err := storage.Open(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	log.Printf("[db] opened %s", dbPath)

	// ── Subsystems ────────────────────────────────────────────────────────────
	sysCollector := metrics.NewCollector()
	alertEngine := alerts.NewEngine(db)
	checker := urlcheck.NewChecker(db)
	tunnelDetector := tunnel.NewDetector(db)
	secMonitor := security.NewMonitor(db)
	svcMonitor := services.NewMonitor(cfg.Services)
	broker := sse.NewBroker()
	handler := httpserver.NewHandler(db, checker, alertEngine)

	// Load thresholds + restore active alerts
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := alertEngine.LoadThresholds(ctx); err != nil {
		log.Printf("[alerts] load thresholds: %v", err)
	}

	if err := checker.Start(ctx); err != nil {
		log.Printf("[urlcheck] start: %v", err)
	}

	// ── Background goroutines ─────────────────────────────────────────────────
	done := make(chan struct{})
	go broker.Run(done)
	go runCollector(ctx, cfg, db, broker, handler, sysCollector, alertEngine, checker, tunnelDetector, secMonitor, svcMonitor)
	go runPurge(ctx, db)
	go relayURLResults(ctx, checker, broker)

	// ── HTTP server ───────────────────────────────────────────────────────────
	mux := http.NewServeMux()
	httpserver.SetupRoutes(mux, handler, broker, cfg, webpkg.FS)

	srv := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // SSE needs unlimited write timeout
		IdleTimeout:  60 * time.Second,
	}

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		fmt.Printf("\n  monitoring24 running at http://%s\n\n", displayAddr(cfg.Host, cfg.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	<-sigCh
	log.Println("[server] shutting down...")
	close(done)
	cancel()

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutCancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Printf("[server] shutdown error: %v", err)
	}
	log.Println("[server] stopped")
}

func runCollector(
	ctx context.Context,
	_ *config.Config,
	_ *storage.DB,
	broker *sse.Broker,
	handler *httpserver.Handler,
	sysCollector *metrics.Collector,
	alertEngine *alerts.Engine,
	checker *urlcheck.Checker,
	tunnelDetector *tunnel.Detector,
	secMonitor *security.Monitor,
	svcMonitor *services.Monitor,
) {
	fastTick := time.NewTicker(2 * time.Second)
	slowTick := time.NewTicker(30 * time.Second)
	defer fastTick.Stop()
	defer slowTick.Stop()

	for {
		select {
		case <-fastTick.C:
			// System metrics
			sysnap, err := sysCollector.Collect(ctx)
			if err != nil {
				log.Printf("[metrics] system: %v", err)
			}
			appSnap, _ := metrics.CollectApp(ctx, startTime)
			netSnap, _ := metrics.CollectNetwork(ctx)
			tunnelStatus, _ := tunnelDetector.Collect(ctx)

			// Alert evaluation
			if sysnap != nil {
				diskAlerts := make([]alerts.DiskAlert, len(sysnap.Disks))
				for i, d := range sysnap.Disks {
					diskAlerts[i] = alerts.DiskAlert{Mountpoint: d.Mountpoint, Percent: d.Percent}
				}
				urlSummaries := checker.Summaries()
				urlAlerts := make([]alerts.URLAlert, 0, len(urlSummaries))
				for _, s := range urlSummaries {
					up := s.LastResult == nil || s.LastResult.Up
					urlAlerts = append(urlAlerts, alerts.URLAlert{
						ID:    s.Check.ID,
						URL:   s.Check.URL,
						Label: s.Check.Label,
						Up:    up,
					})
				}
				fired, _ := alertEngine.Evaluate(ctx, alerts.AllMetrics{
					CPUPercent:  sysnap.CPUPercent,
					RAMPercent:  sysnap.MemPercent,
					SwapPercent: sysnap.SwapPercent,
					Disks:       diskAlerts,
					URLStats:    urlAlerts,
				})
				for _, a := range fired {
					broker.BroadcastJSON("alert", a)
				}
			}

			// Broadcast metrics snapshot
			payload := map[string]any{
				"system":  sysnap,
				"app":     appSnap,
				"network": netSnap,
			}
			if data, err := json.Marshal(payload); err == nil {
				broker.Broadcast("metrics", data)
			}

			if tunnelStatus != nil {
				broker.BroadcastJSON("tunnel", tunnelStatus)
			}

			handler.UpdateLatest(httpserver.LatestData{
				System:  sysnap,
				App:     appSnap,
				Network: netSnap,
				Tunnel:  tunnelStatus,
			})

		case <-slowTick.C:
			// Slower subsystems
			_ = secMonitor.Parse(ctx)
			secSnap, _ := secMonitor.Snapshot(ctx)
			svcSnap, _ := svcMonitor.Collect(ctx)

			if svcSnap != nil {
				broker.BroadcastJSON("services", svcSnap)
			}

			handler.UpdateLatest(httpserver.LatestData{
				Security: secSnap,
				Services: svcSnap,
			})

		case <-ctx.Done():
			return
		}
	}
}

func runPurge(ctx context.Context, db *storage.DB) {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := db.Purge(ctx); err != nil {
				log.Printf("[purge] error: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func relayURLResults(ctx context.Context, checker *urlcheck.Checker, broker *sse.Broker) {
	for {
		select {
		case result := <-checker.ResultCh:
			broker.BroadcastJSON("url_result", result)
		case <-ctx.Done():
			return
		}
	}
}

func displayAddr(host string, port int) string {
	host = strings.TrimSpace(host)
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%d", host, port)
}
