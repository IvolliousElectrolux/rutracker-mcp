package rutracker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCookieHeader(t *testing.T) {
	cookies := parseCookieHeader(" bb_session=abc123; theme=dark; broken; =novalue; spaced = x ")
	got := map[string]string{}
	for _, cookie := range cookies {
		got[cookie.Name] = cookie.Value
	}
	if got["bb_session"] != "abc123" || got["theme"] != "dark" || got["spaced"] != "x" {
		t.Fatalf("cookies = %#v", got)
	}
	if _, ok := got["broken"]; ok {
		t.Fatal("kept a part without '='")
	}
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
}

func TestCookieFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookies.json")
	if err := os.WriteFile(path, []byte("[\n  {\"name\": \"bb_session\", \"value\": \"from-disk\"}\n]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ok, err := SessionFileHas(path)
	if err != nil || !ok {
		t.Fatalf("SessionFileHas = %v, %v", ok, err)
	}
	cookies, err := readCookieFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cookies) != 1 || cookies[0].Name != "bb_session" || cookies[0].Value != "from-disk" {
		t.Fatalf("cookies = %#v", cookies)
	}

	missing, err := SessionFileHas(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil || missing {
		t.Fatalf("missing file = %v, %v", missing, err)
	}
}
