package cheapshark

import (
	"context"
	"regexp"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes cheapshark as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/cheapshark-cli/cheapshark"
//
// The init below registers it; the host then dereferences cheapshark:// URIs
// by routing to the operations Register installs. The same Domain also builds
// the standalone cheapshark binary (see cli.NewApp), so the binary and a host
// share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the cheapshark driver.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against,
// and the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "cheapshark",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "cheapshark",
			Short:  "A command line for CheapShark PC game deals.",
			Long: `A command line for CheapShark PC game deals.

cheapshark reads public CheapShark data over plain HTTPS, shapes it into
clean records, and prints output that pipes into the rest of your tools. No
API key, nothing to run alongside it.`,
			Site: Host,
			Repo: "https://github.com/tamnd/cheapshark-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	// deals: browse current deals by store / price ceiling.
	kit.Handle(app, kit.OpMeta{
		Name:    "deals",
		Group:   "read",
		List:    true,
		Summary: "Browse PC game deals",
	}, listDeals)

	// search: find games by title.
	kit.Handle(app, kit.OpMeta{
		Name:    "search",
		Group:   "read",
		List:    true,
		Summary: "Search games by title",
		Args:    []kit.Arg{{Name: "title", Help: "game title to search for"}},
	}, searchGames)

	// stores: list all stores tracked by CheapShark.
	kit.Handle(app, kit.OpMeta{
		Name:    "stores",
		Group:   "read",
		List:    true,
		Summary: "List all stores tracked by CheapShark",
	}, listStores)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type dealsInput struct {
	Store    string  `kit:"flag" help:"store ID (default 1 = Steam)"`
	MaxPrice float64 `kit:"flag" help:"upper price limit (0 = no limit)"`
	Limit    int     `kit:"flag,inherit" help:"max results"`
	Client   *Client `kit:"inject"`
}

type searchInput struct {
	Title  string  `kit:"arg" help:"game title to search for"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

type storesInput struct {
	Client *Client `kit:"inject"`
}

// --- handlers ---

func listDeals(ctx context.Context, in dealsInput, emit func(*Deal) error) error {
	store := in.Store
	if store == "" {
		store = "1"
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 25
	}
	deals, err := in.Client.ListDeals(ctx, store, in.MaxPrice, limit)
	if err != nil {
		return mapErr(err)
	}
	for i := range deals {
		if err := emit(&deals[i]); err != nil {
			return err
		}
	}
	return nil
}

func searchGames(ctx context.Context, in searchInput, emit func(*GameResult) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	results, err := in.Client.SearchGames(ctx, in.Title, limit)
	if err != nil {
		return mapErr(err)
	}
	for i := range results {
		if err := emit(&results[i]); err != nil {
			return err
		}
	}
	return nil
}

func listStores(ctx context.Context, in storesInput, emit func(*Store) error) error {
	stores, err := in.Client.ListStores(ctx)
	if err != nil {
		return mapErr(err)
	}
	for i := range stores {
		if err := emit(&stores[i]); err != nil {
			return err
		}
	}
	return nil
}

// --- URI driver ---

var allDigits = regexp.MustCompile(`^\d+$`)

// Classify turns any accepted input into (type, id) for URI resolution.
// All-digit inputs are treated as gameIDs; anything else is a query.
func (Domain) Classify(input string) (uriType, id string, err error) {
	if input == "" {
		return "", "", errs.Usage("empty cheapshark reference")
	}
	if allDigits.MatchString(input) {
		return "gameid", input, nil
	}
	return "query", input, nil
}

// Locate returns the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "gameid":
		return "https://www.cheapshark.com/redirect?dealID=" + id, nil
	case "query":
		return "https://www.cheapshark.com/", nil
	default:
		return "", errs.Usage("cheapshark has no resource type %q", uriType)
	}
}

// mapErr converts a library error into the kit error kind.
func mapErr(err error) error {
	return err
}
