package cheapshark

import (
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring, which need no network.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "cheapshark" {
		t.Errorf("Scheme = %q, want cheapshark", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "cheapshark" {
		t.Errorf("Identity.Binary = %q, want cheapshark", info.Identity.Binary)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		in  string
		typ string
		id  string
	}{
		{"186450", "gameid", "186450"},
		{"hades", "query", "hades"},
		{"half-life 2", "query", "half-life 2"},
		{"123456", "gameid", "123456"},
	}
	for _, tc := range cases {
		typ, id, err := Domain{}.Classify(tc.in)
		if err != nil || typ != tc.typ || id != tc.id {
			t.Errorf("Classify(%q) = (%q, %q, %v), want (%q, %q, nil)",
				tc.in, typ, id, err, tc.typ, tc.id)
		}
	}
}

func TestClassifyEmpty(t *testing.T) {
	_, _, err := Domain{}.Classify("")
	if err == nil {
		t.Error("Classify(\"\") should return error")
	}
}

func TestLocate(t *testing.T) {
	got, err := Domain{}.Locate("gameid", "186450")
	want := "https://www.cheapshark.com/redirect?dealID=186450"
	if err != nil || got != want {
		t.Errorf("Locate(gameid) = (%q, %v), want (%q, nil)", got, err, want)
	}

	got, err = Domain{}.Locate("query", "hades")
	want = "https://www.cheapshark.com/"
	if err != nil || got != want {
		t.Errorf("Locate(query) = (%q, %v), want (%q, nil)", got, err, want)
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("unknown", "foo")
	if err == nil {
		t.Error("Locate(unknown) should return error")
	}
}

// TestHostWiring checks the domain is mounted and ResolveOn routes correctly.
func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := h.Domain("cheapshark"); !ok {
		t.Fatal("cheapshark not mounted on host")
	}

	// ResolveOn with a numeric ID should produce a cheapshark:// gameid URI.
	got, err := h.ResolveOn("cheapshark", "186450")
	if err != nil {
		t.Fatalf("ResolveOn(186450): %v", err)
	}
	if got.Scheme != "cheapshark" {
		t.Errorf("ResolveOn scheme = %q, want cheapshark", got.Scheme)
	}

	// ResolveOn with a query string should also produce a cheapshark:// URI.
	got, err = h.ResolveOn("cheapshark", "hades")
	if err != nil {
		t.Fatalf("ResolveOn(hades): %v", err)
	}
	if got.Scheme != "cheapshark" {
		t.Errorf("ResolveOn scheme = %q, want cheapshark", got.Scheme)
	}
}
