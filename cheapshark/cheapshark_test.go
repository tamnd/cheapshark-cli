package cheapshark_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tamnd/cheapshark-cli/cheapshark"
)

// newTestClient returns a Client wired to the given httptest server with
// pacing and retries configured for fast tests.
func newTestClient(srv *httptest.Server) *cheapshark.Client {
	c := cheapshark.NewClient()
	c.BaseURL = srv.URL
	c.Rate = 0
	return c
}

func TestGet_UserAgent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent")
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := cheapshark.NewClient()
	c.Rate = 0
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestGetRetriesOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	c := cheapshark.NewClient()
	c.Rate = 0
	c.Retries = 5

	start := time.Now()
	body, err := c.Get(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "recovered" {
		t.Errorf("body = %q after retries", body)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}

func TestListDeals(t *testing.T) {
	payload := []map[string]any{
		{
			"title":           "Hades",
			"dealID":          "abc123",
			"storeID":         "1",
			"gameID":          "186450",
			"salePrice":       "9.99",
			"normalPrice":     "24.99",
			"savings":         "60.02",
			"metacriticScore": "93",
			"steamAppID":      "1145360",
			"dealRating":      "9.5",
		},
	}
	body, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/1.0/deals" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	deals, err := c.ListDeals(context.Background(), "1", 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(deals) != 1 {
		t.Fatalf("got %d deals, want 1", len(deals))
	}
	d := deals[0]
	if d.Title != "Hades" {
		t.Errorf("Title = %q, want Hades", d.Title)
	}
	if d.SalePrice != "9.99" {
		t.Errorf("SalePrice = %q, want 9.99", d.SalePrice)
	}
	if d.Savings != "60.02" {
		t.Errorf("Savings = %q, want 60.02", d.Savings)
	}
}

func TestSearchGames(t *testing.T) {
	payload := map[string]any{
		"146": map[string]any{
			"gameID":     "146",
			"steamAppID": "220",
			"cheapest":   "0.49",
		},
	}
	body, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/1.0/games" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("title") == "" {
			t.Error("missing title query param")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	results, err := c.SearchGames(context.Background(), "half-life", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	r := results[0]
	if r.GameID != "146" {
		t.Errorf("GameID = %q, want 146", r.GameID)
	}
	if r.Cheapest != "0.49" {
		t.Errorf("Cheapest = %q, want 0.49", r.Cheapest)
	}
}

func TestListStores(t *testing.T) {
	payload := []map[string]any{
		{"storeID": "1", "storeName": "Steam", "isActive": 1},
		{"storeID": "25", "storeName": "Epic Games Store", "isActive": 1},
	}
	body, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/1.0/stores" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	stores, err := c.ListStores(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(stores) != 2 {
		t.Fatalf("got %d stores, want 2", len(stores))
	}
	if stores[0].StoreName != "Steam" {
		t.Errorf("StoreName[0] = %q, want Steam", stores[0].StoreName)
	}
}
