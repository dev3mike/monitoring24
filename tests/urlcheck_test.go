package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/masoudx/monitoring24/internal/storage"
	"github.com/masoudx/monitoring24/internal/urlcheck"
)

func TestURLCheck_AddRunsImmediateCheck(t *testing.T) {
	// given
	db := openTestDB(t)
	c := urlcheck.NewChecker(db)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// when
	ch, err := c.Add(ctx, storage.URLCheck{
		URL:             srv.URL,
		Label:           "t",
		IntervalSeconds: 300,
		TimeoutSeconds:  5,
		Enabled:         true,
		CreatedAt:       time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// then
	select {
	case res := <-c.ResultCh:
		if res.CheckID != ch.ID || !res.Up {
			t.Fatalf("unexpected result: %+v", res)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for first check result")
	}
}

func TestURLCheck_HTTPErrorMarkedDown(t *testing.T) {
	// given
	db := openTestDB(t)
	c := urlcheck.NewChecker(db)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	// when
	ch, err := c.Add(ctx, storage.URLCheck{
		URL:             srv.URL,
		Label:           "",
		IntervalSeconds: 300,
		TimeoutSeconds:  5,
		Enabled:         true,
		CreatedAt:       time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	// then
	select {
	case res := <-c.ResultCh:
		if res.CheckID != ch.ID || res.Up {
			t.Fatalf("expected down for 503, got %+v", res)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

func TestURLCheck_RemoveStopsCheck(t *testing.T) {
	// given
	db := openTestDB(t)
	c := urlcheck.NewChecker(db)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	ch, _ := c.Add(ctx, storage.URLCheck{
		URL:             srv.URL,
		Label:           "",
		IntervalSeconds: 120,
		TimeoutSeconds:  5,
		Enabled:         true,
		CreatedAt:       time.Now(),
	})
	<-c.ResultCh

	// when
	if err := c.Remove(ch.ID); err != nil {
		t.Fatal(err)
	}

	// then
	if _, ok := c.GetSummary(ch.ID); ok {
		t.Fatal("expected summary removed")
	}
}
