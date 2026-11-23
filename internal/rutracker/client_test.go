package rutracker

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func TestParseBases(t *testing.T) {
	bases, err := parseBases("")
	if err != nil {
		t.Fatal(err)
	}
	if len(bases) != 2 || bases[0].Host != "rutracker.org" || bases[1].Host != "rutracker.net" {
		t.Fatalf("bases = %#v", bases)
	}
	if _, err := parseBases("http://rutracker.net/forum/"); !errors.Is(err, ErrBaseURL) {
		t.Fatalf("http base: %v", err)
	}
	pinned, err := parseBases("https://rutracker.net/forum")
	if err != nil {
		t.Fatal(err)
	}
	if len(pinned) != 1 || pinned[0].Host != "rutracker.net" || pinned[0].Path != "/forum/" {
		t.Fatalf("pinned = %#v", pinned[0])
	}
}

func TestEndpointRaw(t *testing.T) {
	base, err := url.Parse("https://rutracker.net/forum/")
	if err != nil {
		t.Fatal(err)
	}
	got, err := endpointRaw(base, "tracker.php", "nm=a")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://rutracker.net/forum/tracker.php?nm=a" {
		t.Fatalf("endpoint = %s", got)
	}
}

func TestProxyTransport(t *testing.T) {
	plain, err := proxyTransport("")
	if err != nil {
		t.Fatal(err)
	}
	if plain.(*http.Transport).Proxy != nil {
		t.Fatal("empty proxy should ignore HTTP_PROXY")
	}
	for _, raw := range []string{"http://127.0.0.1:7890", "https://127.0.0.1:7890", "socks5://127.0.0.1:7890"} {
		tr, err := proxyTransport(raw)
		if err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if tr == nil {
			t.Fatalf("%s: nil transport", raw)
		}
	}
	if _, err := proxyTransport("ftp://127.0.0.1:21"); err == nil {
		t.Fatal("expected ftp to be rejected")
	}
}
