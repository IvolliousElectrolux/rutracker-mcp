package rutracker

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestParseCaptcha(t *testing.T) {
	base, err := url.Parse("https://rutracker.net/forum/")
	if err != nil {
		t.Fatal(err)
	}
	page, err := encode1251(`<form>
<input type="hidden" name="redirect" value="index.php">
<img src="https://static.rutracker.cc/captcha/abc.jpg?1" alt="pic">
<input type="hidden" name="cap_sid" value="YKx6FsRW3UsFgh5uZ2VY">
<input type="text" name="cap_code_bbc612279a41666e6c0c4ce507b79361">
<input type="submit" name="login" value="Вход">
</form>`)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := parseCaptcha([]byte(page), base)
	if err != nil || !ok {
		t.Fatalf("parse = %v, %v", ok, err)
	}
	if got.Sid != "YKx6FsRW3UsFgh5uZ2VY" || got.Field != "cap_code_bbc612279a41666e6c0c4ce507b79361" {
		t.Fatalf("fields = %#v", got)
	}
	if got.Login != "Вход" || got.Redirect != "index.php" {
		t.Fatalf("login = %q redirect = %q", got.Login, got.Redirect)
	}
	if got.Image != "https://static.rutracker.cc/captcha/abc.jpg?1" {
		t.Fatalf("image = %s", got.Image)
	}

	evil, err := encode1251(`<input name="cap_sid" value="SID"><input name="cap_code_abc"><img src="https://evil.example/captcha/a.jpg">`)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := parseCaptcha([]byte(evil), base); err == nil {
		t.Fatal("expected a rejected captcha image")
	}
	plain, err := encode1251(`<html><body>nope</body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := parseCaptcha([]byte(plain), base); err != nil || ok {
		t.Fatalf("plain page = %v, %v", ok, err)
	}
}

func TestNormalizeOCR(t *testing.T) {
	if got := normalizeOCR(" a-b_12 "); got != "ab12" {
		t.Fatalf("normalize = %q", got)
	}
	if got := normalizeOCR("汉字"); got != "" {
		t.Fatalf("letters outside ascii = %q", got)
	}
	if got := normalizeOCR(strings.Repeat("a", 17)); got != "" {
		t.Fatalf("long text = %q", got)
	}
}

func TestLoginSubmitsCaptcha(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	page, err := encode1251(`<form>
<input type="hidden" name="redirect" value="index.php">
<img src="/captcha/e632.jpg" alt="pic">
<input type="hidden" name="cap_sid" value="YKx6FsRW3UsFgh5uZ2VY">
<input type="text" name="cap_code_bbc612279a41666e6c0c4ce507b79361">
<input type="submit" name="login" value="Вход">
</form>`)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var posts []string
	mux := http.NewServeMux()
	mux.HandleFunc("/forum/login.php", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		posts = append(posts, string(body))
		n := len(posts)
		mu.Unlock()
		if n == 1 {
			_, _ = w.Write([]byte(page))
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "bb_session", Value: "ok", Path: "/"})
	})
	mux.HandleFunc("/captcha/e632.jpg", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(png)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	base, err := url.Parse(srv.URL + "/forum/")
	if err != nil {
		t.Fatal(err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	hc := srv.Client()
	hc.Jar = jar
	hc.Timeout = requestTimeout
	client := &Client{
		bases:     []*url.URL{base},
		http:      hc,
		jar:       jar,
		username:  "user",
		password:  "secret",
		userAgent: "test",
		solver:    staticSolver{text: "ab12"},
	}
	if err := client.login(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(posts) != 2 {
		t.Fatalf("posts = %d", len(posts))
	}
	if strings.Contains(posts[0], "cap_sid=") || !strings.Contains(posts[0], "login=%E2%F5%EE%E4") {
		t.Fatalf("first post = %s", posts[0])
	}
	second := posts[1]
	for _, part := range []string{
		"cap_sid=YKx6FsRW3UsFgh5uZ2VY",
		"cap_code_bbc612279a41666e6c0c4ce507b79361=ab12",
		"login=%C2%F5%EE%E4",
		"redirect=index.php",
		"login_username=user",
	} {
		if !strings.Contains(second, part) {
			t.Fatalf("second post missing %s: %s", part, second)
		}
	}
}

type staticSolver struct {
	text string
	err  error
}

func (s staticSolver) Solve(context.Context, []byte) (string, error) {
	return s.text, s.err
}
