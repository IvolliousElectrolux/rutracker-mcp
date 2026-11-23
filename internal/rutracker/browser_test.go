package rutracker

import (
	"net/url"
	"path/filepath"
	"testing"
)

func TestResolveBrowserMissing(t *testing.T) {
	if _, err := resolveBrowser(filepath.Join(t.TempDir(), "missing.exe")); err == nil {
		t.Fatal("expected a missing browser to be rejected")
	}
}

func TestChromeProxyServer(t *testing.T) {
	got, err := chromeProxyServer("socks5h://127.0.0.1:7890")
	if err != nil {
		t.Fatal(err)
	}
	if got != "socks5://127.0.0.1:7890" {
		t.Fatalf("proxy = %s", got)
	}
	if _, err := chromeProxyServer("ftp://127.0.0.1:21"); err == nil {
		t.Fatal("expected ftp to be rejected")
	}
}

func TestBrowserHelpers(t *testing.T) {
	if browserSeedCookie("cf_clearance") || browserSeedCookie("__cf_bm") {
		t.Fatal("cloudflare cookies must be issued by this browser")
	}
	if !browserSeedCookie("bb_session") {
		t.Fatal("session cookie should be seeded")
	}
	if !cloudflareChallenge(403, []byte("<title>Just a moment...</title>")) {
		t.Fatal("expected a challenge page")
	}
	if cloudflareChallenge(200, []byte("Just a moment")) {
		t.Fatal("a normal page is not a challenge")
	}
	page, err := url.Parse("https://rutracker.net/forum/tracker.php")
	if err != nil {
		t.Fatal(err)
	}
	if originStart(page) != "https://rutracker.net/forum/index.php" {
		t.Fatalf("origin = %s", originStart(page))
	}
	image, err := url.Parse("https://static.rutracker.cc/captcha/a.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if originStart(image) != "https://static.rutracker.cc/" {
		t.Fatalf("image origin = %s", originStart(image))
	}
}
