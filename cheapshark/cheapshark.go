// Package cheapshark is the library behind the cheapshark command line:
// the HTTP client, request shaping, and the typed data models for the
// CheapShark PC game deal price aggregator API.
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests, and retries transient failures (429 and 5xx).
package cheapshark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// DefaultUserAgent identifies the client to CheapShark.
const DefaultUserAgent = "cheapshark-cli/0.1 (tamnd87@gmail.com)"

// Host is the site this client talks to.
const Host = "cheapshark.com"

// Config holds the tunables for a Client.
type Config struct {
	BaseURL   string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
	UserAgent string
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		BaseURL:   "https://www.cheapshark.com",
		Rate:      300 * time.Millisecond,
		Retries:   3,
		Timeout:   15 * time.Second,
		UserAgent: DefaultUserAgent,
	}
}

// storeNames maps the common CheapShark storeIDs to human-readable names.
var storeNames = map[string]string{
	"1":  "Steam",
	"2":  "GamersGate",
	"3":  "GreenManGaming",
	"7":  "GOG",
	"11": "Humble",
	"13": "Fanatical",
	"25": "Epic Games",
}

// storeName returns the display name for a storeID, falling back to the ID itself.
func storeName(id string) string {
	if name, ok := storeNames[id]; ok {
		return name
	}
	return id
}

// Client talks to the CheapShark API over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	BaseURL   string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// NewClient returns a Client with DefaultConfig values.
func NewClient() *Client {
	cfg := DefaultConfig()
	return &Client{
		HTTP:      &http.Client{Timeout: cfg.Timeout},
		UserAgent: cfg.UserAgent,
		BaseURL:   cfg.BaseURL,
		Rate:      cfg.Rate,
		Retries:   cfg.Retries,
	}
}

// --- wire types (match CheapShark JSON keys exactly) ---

type wireDeal struct {
	Title       string `json:"title"`
	DealID      string `json:"dealID"`
	StoreID     string `json:"storeID"`
	GameID      string `json:"gameID"`
	SalePrice   string `json:"salePrice"`
	NormalPrice string `json:"normalPrice"`
	Savings     string `json:"savings"`
	DealRating  string `json:"dealRating"`
}

type wireStore struct {
	StoreID   string `json:"storeID"`
	StoreName string `json:"storeName"`
	IsActive  int    `json:"isActive"`
}

type wireGameResult struct {
	GameID   string `json:"gameID"`
	External string `json:"external"`
	Cheapest string `json:"cheapest"`
}

type wireGameInfo struct {
	Title     string `json:"title"`
	SteamAppID string `json:"steamAppID"`
}

type wireGameDeal struct {
	StoreID     string `json:"storeID"`
	DealID      string `json:"dealID"`
	Price       string `json:"price"`
	RetailPrice string `json:"retailPrice"`
	Savings     string `json:"savings"`
}

type wireGameDetails struct {
	Info             wireGameInfo   `json:"info"`
	CheapestPriceEver struct {
		Price string `json:"price"`
		Date  int64  `json:"date"`
	} `json:"cheapestPriceEver"`
	Deals []wireGameDeal `json:"deals"`
}

// --- public output types ---

// Deal is a single game deal from the CheapShark /deals endpoint.
type Deal struct {
	Title   string `kit:"id" json:"title"`
	Store   string `json:"store"`
	Sale    string `json:"sale_price"`
	Normal  string `json:"normal_price"`
	Savings string `json:"savings_pct"`
	Rating  string `json:"rating"`
	DealID  string `json:"deal_id"`
}

// GameResult is one entry from the CheapShark /games search endpoint.
type GameResult struct {
	GameID   string `kit:"id" json:"game_id"`
	Title    string `json:"title"`
	Cheapest string `json:"cheapest"`
}

// GameDeal is one deal from a game's detail page.
type GameDeal struct {
	Title   string `kit:"id" json:"title"`
	Store   string `json:"store"`
	Price   string `json:"price"`
	Retail  string `json:"retail"`
	Savings string `json:"savings_pct"`
	DealID  string `json:"deal_id"`
}

// Store is one store from the CheapShark /stores endpoint.
type Store struct {
	ID     string `kit:"id" json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// --- client methods ---

// ListDeals returns deals sorted and filtered per the given parameters.
// storeID "" omits the storeID filter. sortBy defaults to "Recent".
func (c *Client) ListDeals(ctx context.Context, storeID, sortBy string, limit int) ([]Deal, error) {
	q := url.Values{}
	if storeID != "" {
		q.Set("storeID", storeID)
	}
	if sortBy != "" {
		q.Set("sortBy", sortBy)
	}
	if limit > 0 {
		q.Set("pageSize", strconv.Itoa(limit))
	}

	raw, err := c.Get(ctx, c.BaseURL+"/api/1.0/deals?"+q.Encode())
	if err != nil {
		return nil, err
	}

	var wire []wireDeal
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("cheapshark deals decode: %w", err)
	}

	out := make([]Deal, len(wire))
	for i, w := range wire {
		sav := formatSavings(w.Savings)
		out[i] = Deal{
			Title:   w.Title,
			Store:   storeName(w.StoreID),
			Sale:    w.SalePrice,
			Normal:  w.NormalPrice,
			Savings: sav,
			Rating:  w.DealRating,
			DealID:  w.DealID,
		}
	}
	return out, nil
}

// SearchGames searches games by title.
// The CheapShark API returns an array of game stubs.
func (c *Client) SearchGames(ctx context.Context, title string, limit int) ([]GameResult, error) {
	q := url.Values{}
	q.Set("title", title)
	q.Set("exact", "0")
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}

	raw, err := c.Get(ctx, c.BaseURL+"/api/1.0/games?"+q.Encode())
	if err != nil {
		return nil, err
	}

	// The API returns an array for title search.
	if len(raw) > 0 && raw[0] == '[' {
		var wire []wireGameResult
		if err := json.Unmarshal(raw, &wire); err != nil {
			return nil, fmt.Errorf("cheapshark games decode: %w", err)
		}
		out := make([]GameResult, len(wire))
		for i, w := range wire {
			out[i] = GameResult{
				GameID:   w.GameID,
				Title:    w.External,
				Cheapest: w.Cheapest,
			}
		}
		return out, nil
	}

	// Fall back to map form (documented format, keys = gameIDs).
	var wire map[string]wireGameResult
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("cheapshark games decode: %w", err)
	}
	out := make([]GameResult, 0, len(wire))
	for _, w := range wire {
		out = append(out, GameResult{
			GameID:   w.GameID,
			Title:    w.External,
			Cheapest: w.Cheapest,
		})
	}
	return out, nil
}

// GetGame returns all deals for a single game by its CheapShark game ID.
func (c *Client) GetGame(ctx context.Context, gameID string) ([]GameDeal, error) {
	raw, err := c.Get(ctx, c.BaseURL+"/api/1.0/games?id="+url.QueryEscape(gameID))
	if err != nil {
		return nil, err
	}

	var wire wireGameDetails
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("cheapshark game decode: %w", err)
	}

	title := wire.Info.Title
	out := make([]GameDeal, len(wire.Deals))
	for i, d := range wire.Deals {
		out[i] = GameDeal{
			Title:   title,
			Store:   storeName(d.StoreID),
			Price:   d.Price,
			Retail:  d.RetailPrice,
			Savings: formatSavings(d.Savings),
			DealID:  d.DealID,
		}
	}
	return out, nil
}

// ListStores returns all stores known to CheapShark. Active-only filtering
// is done by the caller.
func (c *Client) ListStores(ctx context.Context) ([]Store, error) {
	raw, err := c.Get(ctx, c.BaseURL+"/api/1.0/stores")
	if err != nil {
		return nil, err
	}

	var wire []wireStore
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("cheapshark stores decode: %w", err)
	}

	out := make([]Store, 0, len(wire))
	for _, w := range wire {
		if w.IsActive != 1 {
			continue
		}
		out = append(out, Store{
			ID:     w.StoreID,
			Name:   w.StoreName,
			Active: true,
		})
	}
	return out, nil
}

// formatSavings turns a raw savings string like "60.024" into "60%".
func formatSavings(s string) string {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	return fmt.Sprintf("%.0f%%", f)
}

// --- transport ---

// Get fetches rawURL and returns the response body. It paces and retries
// according to the client's settings.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}
