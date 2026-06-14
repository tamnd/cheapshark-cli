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
			"title":       "Hades",
			"dealID":      "abc123",
			"storeID":     "1",
			"gameID":      "186450",
			"salePrice":   "9.99",
			"normalPrice": "24.99",
			"savings":     "60.024",
			"dealRating":  "9.5",
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
	deals, err := c.ListDeals(context.Background(), "1", "Recent", 5)
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
	if d.Store != "Steam" {
		t.Errorf("Store = %q, want Steam", d.Store)
	}
	if d.Sale != "9.99" {
		t.Errorf("Sale = %q, want 9.99", d.Sale)
	}
	if d.Savings != "60%" {
		t.Errorf("Savings = %q, want 60%%", d.Savings)
	}
	if d.Rating != "9.5" {
		t.Errorf("Rating = %q, want 9.5", d.Rating)
	}
}

func TestListDealsUnknownStore(t *testing.T) {
	payload := []map[string]any{
		{
			"title":       "Some Game",
			"dealID":      "xyz",
			"storeID":     "99",
			"gameID":      "999",
			"salePrice":   "4.99",
			"normalPrice": "14.99",
			"savings":     "66.666",
			"dealRating":  "7.2",
		},
	}
	body, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	deals, err := c.ListDeals(context.Background(), "", "Recent", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(deals) != 1 {
		t.Fatalf("got %d deals, want 1", len(deals))
	}
	// Unknown store ID falls back to the raw ID.
	if deals[0].Store != "99" {
		t.Errorf("Store = %q, want 99 for unknown storeID", deals[0].Store)
	}
	if deals[0].Savings != "67%" {
		t.Errorf("Savings = %q, want 67%%", deals[0].Savings)
	}
}

func TestSearchGames_Array(t *testing.T) {
	payload := []map[string]any{
		{
			"gameID":   "612",
			"external": "Portal",
			"cheapest": "0.49",
		},
		{
			"gameID":   "613",
			"external": "Portal 2",
			"cheapest": "1.99",
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
	results, err := c.SearchGames(context.Background(), "portal", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].GameID != "612" {
		t.Errorf("GameID = %q, want 612", results[0].GameID)
	}
	if results[0].Title != "Portal" {
		t.Errorf("Title = %q, want Portal", results[0].Title)
	}
	if results[0].Cheapest != "0.49" {
		t.Errorf("Cheapest = %q, want 0.49", results[0].Cheapest)
	}
}

func TestSearchGames_Map(t *testing.T) {
	// Some API responses use the map form (key = gameID).
	payload := map[string]any{
		"146": map[string]any{
			"gameID":   "146",
			"external": "Half-Life",
			"cheapest": "0.99",
		},
	}
	body, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	if results[0].GameID != "146" {
		t.Errorf("GameID = %q, want 146", results[0].GameID)
	}
	if results[0].Title != "Half-Life" {
		t.Errorf("Title = %q, want Half-Life", results[0].Title)
	}
}

func TestGetGame(t *testing.T) {
	payload := map[string]any{
		"info": map[string]any{
			"title":      "Portal",
			"steamAppID": "400",
		},
		"cheapestPriceEver": map[string]any{
			"price": "0.99",
			"date":  1602806400,
		},
		"deals": []map[string]any{
			{
				"storeID":     "1",
				"dealID":      "deal001",
				"price":       "9.99",
				"retailPrice": "9.99",
				"savings":     "0.000000",
			},
			{
				"storeID":     "7",
				"dealID":      "deal002",
				"price":       "4.99",
				"retailPrice": "9.99",
				"savings":     "50.050050",
			},
		},
	}
	body, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/1.0/games" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("id") != "612" {
			t.Errorf("unexpected id param %q", r.URL.Query().Get("id"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := newTestClient(srv)
	deals, err := c.GetGame(context.Background(), "612")
	if err != nil {
		t.Fatal(err)
	}
	if len(deals) != 2 {
		t.Fatalf("got %d deals, want 2", len(deals))
	}
	if deals[0].Title != "Portal" {
		t.Errorf("Title = %q, want Portal", deals[0].Title)
	}
	if deals[0].Store != "Steam" {
		t.Errorf("Store = %q, want Steam", deals[0].Store)
	}
	if deals[0].Price != "9.99" {
		t.Errorf("Price = %q, want 9.99", deals[0].Price)
	}
	if deals[1].Store != "GOG" {
		t.Errorf("Store = %q, want GOG for storeID 7", deals[1].Store)
	}
	if deals[1].Savings != "50%" {
		t.Errorf("Savings = %q, want 50%%", deals[1].Savings)
	}
	if deals[1].DealID != "deal002" {
		t.Errorf("DealID = %q, want deal002", deals[1].DealID)
	}
}

func TestListStores(t *testing.T) {
	payload := []map[string]any{
		{"storeID": "1", "storeName": "Steam", "isActive": 1},
		{"storeID": "99", "storeName": "Defunct Store", "isActive": 0},
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
	// Only active stores should be returned.
	if len(stores) != 2 {
		t.Fatalf("got %d stores, want 2 (active only)", len(stores))
	}
	if stores[0].ID != "1" {
		t.Errorf("ID = %q, want 1", stores[0].ID)
	}
	if stores[0].Name != "Steam" {
		t.Errorf("Name = %q, want Steam", stores[0].Name)
	}
	if !stores[0].Active {
		t.Error("Active should be true")
	}
	if stores[1].ID != "25" {
		t.Errorf("ID = %q, want 25", stores[1].ID)
	}
}
