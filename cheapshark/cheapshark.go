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
const DefaultUserAgent = "cheapshark-cli/dev (+https://github.com/tamnd/cheapshark-cli)"

// Host is the site this client talks to.
const Host = "cheapshark.com"

// Config holds the tunables for a Client.
type Config struct {
	BaseURL string
	Rate    time.Duration
	Retries int
	Timeout time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		BaseURL: "https://www.cheapshark.com",
		Rate:    0,
		Retries: 3,
		Timeout: 15 * time.Second,
	}
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
		UserAgent: DefaultUserAgent,
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
	MetaScore   string `json:"metacriticScore"`
	SteamAppID  string `json:"steamAppID"`
	DealRating  string `json:"dealRating"`
}

type wireStore struct {
	StoreID   string `json:"storeID"`
	StoreName string `json:"storeName"`
	IsActive  int    `json:"isActive"`
}

type wireGameResult struct {
	GameID     string `json:"gameID"`
	SteamAppID string `json:"steamAppID"`
	Cheapest   string `json:"cheapest"`
}

// --- public output types ---

// Deal is a single game deal from the CheapShark /deals endpoint.
type Deal struct {
	Title       string `kit:"id" json:"title"`
	StoreID     string `json:"store_id"`
	SalePrice   string `json:"sale_price"`
	NormalPrice string `json:"normal_price"`
	Savings     string `json:"savings"`
	DealID      string `json:"deal_id"`
	GameID      string `json:"game_id"`
	SteamAppID  string `json:"steam_app_id"`
	Metacritic  string `json:"metacritic"`
	DealRating  string `json:"deal_rating"`
}

// GameResult is one entry from the CheapShark /games search endpoint.
type GameResult struct {
	GameID     string `kit:"id" json:"game_id"`
	SteamAppID string `json:"steam_app_id"`
	Cheapest   string `json:"cheapest"`
}

// Store is one store from the CheapShark /stores endpoint.
type Store struct {
	StoreID   string `kit:"id" json:"store_id"`
	StoreName string `json:"store_name"`
	IsActive  int    `json:"is_active"`
}

// --- client methods ---

// ListDeals returns deals from the given store filtered by maxPrice (0 = no
// filter). storeID "1" is Steam.
func (c *Client) ListDeals(ctx context.Context, storeID string, maxPrice float64, limit int) ([]Deal, error) {
	q := url.Values{}
	if storeID != "" {
		q.Set("storeID", storeID)
	}
	if maxPrice > 0 {
		q.Set("upperPrice", strconv.FormatFloat(maxPrice, 'f', 2, 64))
	}
	if limit > 0 {
		q.Set("pageSize", strconv.Itoa(limit))
	}
	q.Set("pageNumber", "0")

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
		out[i] = Deal{
			Title:       w.Title,
			StoreID:     w.StoreID,
			SalePrice:   w.SalePrice,
			NormalPrice: w.NormalPrice,
			Savings:     w.Savings,
			DealID:      w.DealID,
			GameID:      w.GameID,
			SteamAppID:  w.SteamAppID,
			Metacritic:  w.MetaScore,
			DealRating:  w.DealRating,
		}
	}
	return out, nil
}

// SearchGames searches games by title.
// The CheapShark API returns either an array or a map depending on the
// query; we handle both formats.
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

	// Try array form first (actual API behavior for title search).
	if len(raw) > 0 && raw[0] == '[' {
		var wire []wireGameResult
		if err := json.Unmarshal(raw, &wire); err != nil {
			return nil, fmt.Errorf("cheapshark games decode: %w", err)
		}
		out := make([]GameResult, len(wire))
		for i, w := range wire {
			out[i] = GameResult{
				GameID:     w.GameID,
				SteamAppID: w.SteamAppID,
				Cheapest:   w.Cheapest,
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
			GameID:     w.GameID,
			SteamAppID: w.SteamAppID,
			Cheapest:   w.Cheapest,
		})
	}
	return out, nil
}

// ListStores returns all stores known to CheapShark.
func (c *Client) ListStores(ctx context.Context) ([]Store, error) {
	raw, err := c.Get(ctx, c.BaseURL+"/api/1.0/stores")
	if err != nil {
		return nil, err
	}

	var wire []wireStore
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("cheapshark stores decode: %w", err)
	}

	out := make([]Store, len(wire))
	for i, w := range wire {
		out[i] = Store{
			StoreID:   w.StoreID,
			StoreName: w.StoreName,
			IsActive:  w.IsActive,
		}
	}
	return out, nil
}

// --- transport ---

// Get fetches url and returns the response body. It paces and retries
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
