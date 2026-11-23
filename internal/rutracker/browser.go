package rutracker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

type browserTransport struct {
	exe      string
	profile  string
	proxy    string
	headless bool

	mu      sync.Mutex
	alloc   context.CancelFunc
	browser context.CancelFunc
	ctx     context.Context
	seeded  bool
	cookies map[string]string
}

func newBrowserTransport(opts Options) (http.RoundTripper, error) {
	exe, err := resolveBrowser(opts.Browser)
	if err != nil {
		return nil, err
	}
	cookieFile := opts.CookieFile
	if cookieFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrBrowser, err.Error())
		}
		cookieFile = filepath.Join(home, ".rutracker-mcp", "cookies.json")
	}
	return &browserTransport{
		exe:      exe,
		profile:  filepath.Join(filepath.Dir(cookieFile), "browser"),
		proxy:    opts.Proxy,
		headless: opts.Headless,
		cookies:  map[string]string{},
	}, nil
}

func (b *browserTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil || req.URL.Host == "" {
		return nil, fmt.Errorf("%w: request URL is missing a host", ErrBrowser)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.ensure(req.Context()); err != nil {
		return nil, err
	}
	if err := b.seed(req); err != nil {
		return nil, err
	}
	if err := b.ensureOrigin(req.Context(), req.URL); err != nil {
		return nil, err
	}
	payload, err := readRequestBody(req)
	if err != nil {
		return nil, err
	}
	resp, body, err := b.fetch(req, payload)
	if err != nil {
		return nil, err
	}
	if cloudflareChallenge(resp.StatusCode, body) {
		fmt.Fprintf(os.Stderr, "rutracker-mcp: 等待浏览器通过 Cloudflare\n")
		if err := b.openAndWait(req.Context(), req.URL.String()); err != nil {
			return nil, err
		}
		resp, body, err = b.fetch(req, payload)
		if err != nil {
			return nil, err
		}
		if cloudflareChallenge(resp.StatusCode, body) {
			return nil, fmt.Errorf("%w: 检查没有通过. 留着打开的浏览器窗口完成它, 然后重试", ErrBrowser)
		}
	}
	if err := b.applyCookieChanges(resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (b *browserTransport) ensure(ctx context.Context) error {
	if b.ctx != nil {
		return nil
	}
	if err := os.MkdirAll(b.profile, 0o700); err != nil {
		return fmt.Errorf("%w: %s", ErrBrowser, err.Error())
	}
	proxyServer, err := chromeProxyServer(b.proxy)
	if err != nil {
		return err
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(b.exe),
		chromedp.UserDataDir(b.profile),
		chromedp.Flag("enable-automation", false),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("window-size", "1100,800"),
	)
	if b.headless {
		opts = append(opts, chromedp.Flag("headless", "new"))
	} else {
		opts = append(opts, chromedp.Flag("headless", false))
	}
	if proxyServer != "" {
		opts = append(opts, chromedp.Flag("proxy-server", proxyServer))
	}
	fmt.Fprintf(os.Stderr, "rutracker-mcp: 使用浏览器访问站点: %s\n", b.exe)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	runCtx, cancel := context.WithTimeout(browserCtx, 45*time.Second)
	err = chromedp.Run(runCtx, chromedp.Navigate("about:blank"))
	cancel()
	if err != nil {
		browserCancel()
		allocCancel()
		return fmt.Errorf("%w: 无法启动浏览器: %s", ErrBrowser, err.Error())
	}
	b.alloc = allocCancel
	b.browser = browserCancel
	b.ctx = browserCtx
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func (b *browserTransport) seed(req *http.Request) error {
	if b.seeded {
		return nil
	}
	actions := make([]chromedp.Action, 0)
	for _, cookie := range req.Cookies() {
		if !browserSeedCookie(cookie.Name) || cookie.Value == "" {
			continue
		}
		page := req.URL.Scheme + "://" + req.URL.Host + "/"
		name, value := cookie.Name, cookie.Value
		actions = append(actions, chromedp.ActionFunc(func(ctx context.Context) error {
			return network.SetCookie(name, value).WithURL(page).Do(ctx)
		}))
	}
	if len(actions) > 0 {
		if err := b.run(req.Context(), actions...); err != nil {
			return err
		}
	}
	current, err := b.cookieMap(req.Context(), req.URL)
	if err != nil {
		return err
	}
	b.cookies = current
	b.seeded = true
	return nil
}

func (b *browserTransport) ensureOrigin(ctx context.Context, target *url.URL) error {
	var loc string
	if err := b.run(ctx, chromedp.Location(&loc)); err != nil {
		return err
	}
	current, err := url.Parse(loc)
	if err == nil && current.Hostname() == target.Hostname() && current.Scheme == target.Scheme {
		return nil
	}
	return b.openAndWait(ctx, originStart(target))
}

func (b *browserTransport) openAndWait(ctx context.Context, raw string) error {
	if err := b.run(ctx, chromedp.Navigate(raw)); err != nil {
		return err
	}
	deadline := time.Now().Add(45 * time.Second)
	for {
		var title string
		if err := b.run(ctx, chromedp.Title(&title)); err != nil {
			return err
		}
		if !challengeTitle(title) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: 检查没有在 45 秒内通过. 留着打开的浏览器窗口完成它, 然后重试", ErrBrowser)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func readRequestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	payload, err := io.ReadAll(io.LimitReader(req.Body, maxTorrentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > maxTorrentBytes {
		return nil, ErrTooLarge
	}
	return payload, nil
}

func (b *browserTransport) fetch(req *http.Request, payload []byte) (*http.Response, []byte, error) {
	job := fetchJob{
		URL:     req.URL.String(),
		Method:  req.Method,
		Headers: map[string]string{},
		Limit:   maxTorrentBytes,
	}
	if len(payload) > 0 {
		job.Body = base64.StdEncoding.EncodeToString(payload)
	}
	for key, values := range req.Header {
		if skipBrowserHeader(key) || len(values) == 0 {
			continue
		}
		job.Headers[key] = values[0]
	}
	expr, err := fetchExpression(job)
	if err != nil {
		return nil, nil, err
	}
	var result fetchResult
	if err := b.run(req.Context(), chromedp.Evaluate(expr, &result, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithAwaitPromise(true).WithReturnByValue(true)
	})); err != nil {
		b.reset()
		return nil, nil, fmt.Errorf("%w: %s", ErrBrowser, err.Error())
	}
	if result.Error == "too-large" {
		return nil, nil, ErrTooLarge
	}
	if result.Error != "" {
		return nil, nil, fmt.Errorf("%w: %s", ErrMirror, result.Error)
	}
	body, err := base64.StdEncoding.DecodeString(result.Body)
	if result.Body != "" && err != nil {
		return nil, nil, fmt.Errorf("%w: 浏览器返回的正文无法解码", ErrBrowser)
	}
	finalURL := req.URL
	if parsed, err := url.Parse(result.URL); err == nil && parsed.Host != "" {
		finalURL = parsed
	}
	header := make(http.Header, len(result.Headers))
	for _, pair := range result.Headers {
		if len(pair) == 2 && pair[0] != "" {
			header.Add(pair[0], pair[1])
		}
	}
	cloned := req.Clone(req.Context())
	cloned.URL = finalURL
	response := &http.Response{
		StatusCode: result.Status,
		Status:     fmt.Sprintf("%d", result.Status),
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    cloned,
	}
	return response, body, nil
}

func (b *browserTransport) applyCookieChanges(resp *http.Response) error {
	if resp.Request == nil || resp.Request.URL == nil {
		return nil
	}
	current, err := b.cookieMap(resp.Request.Context(), resp.Request.URL)
	if err != nil {
		return err
	}
	for key, value := range current {
		if b.cookies[key] == value {
			continue
		}
		name := key
		if cut, _, ok := strings.Cut(key, "|"); ok {
			name = cut
		}
		resp.Header.Add("Set-Cookie", (&http.Cookie{Name: name, Value: value, Path: "/"}).String())
	}
	b.cookies = current
	return nil
}

func (b *browserTransport) cookieMap(ctx context.Context, page *url.URL) (map[string]string, error) {
	var cookies []*network.Cookie
	err := b.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		cookies, err = network.GetCookies().WithURLs([]string{page.Scheme + "://" + page.Host + "/"}).Do(ctx)
		return err
	}))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBrowser, err.Error())
	}
	out := make(map[string]string, len(cookies))
	for _, cookie := range cookies {
		if cookie.Name == "" {
			continue
		}
		out[cookie.Name+"|"+cookie.Domain] = cookie.Value
	}
	return out, nil
}

func (b *browserTransport) run(ctx context.Context, actions ...chromedp.Action) error {
	if b.ctx == nil {
		return fmt.Errorf("%w: 浏览器没有启动", ErrBrowser)
	}
	runCtx, cancel := context.WithCancel(b.ctx)
	defer cancel()
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-stop:
		case <-runCtx.Done():
		}
	}()
	defer close(stop)
	return chromedp.Run(runCtx, actions...)
}

func (b *browserTransport) reset() {
	if b.browser != nil {
		b.browser()
	}
	if b.alloc != nil {
		b.alloc()
	}
	b.ctx = nil
	b.browser = nil
	b.alloc = nil
	b.seeded = false
}

type fetchJob struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body,omitempty"`
	Limit   int               `json:"limit"`
}

type fetchResult struct {
	Status  int        `json:"status"`
	URL     string     `json:"url"`
	Headers [][]string `json:"headers"`
	Body    string     `json:"body"`
	Error   string     `json:"error"`
	Length  int        `json:"length"`
}

func fetchExpression(job fetchJob) (string, error) {
	raw, err := json.Marshal(job)
	if err != nil {
		return "", err
	}
	return `(async () => {
  const job = ` + string(raw) + `;
  try {
    const headers = new Headers();
    Object.keys(job.headers || {}).forEach((key) => headers.set(key, job.headers[key]));
    const init = { method: job.method, headers: headers, credentials: "include", redirect: "follow" };
    if (job.body) {
      const binary = atob(job.body);
      const bytes = new Uint8Array(binary.length);
      for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
      init.body = bytes;
    }
    const response = await fetch(job.url, init);
    const bytes = new Uint8Array(await response.arrayBuffer());
    const outHeaders = [];
    response.headers.forEach((value, key) => outHeaders.push([key, value]));
    if (bytes.length > job.limit) {
      return { status: response.status, url: response.url, headers: outHeaders, body: "", error: "too-large", length: bytes.length };
    }
    let text = "";
    const size = 4096;
    for (let i = 0; i < bytes.length; i += size) {
      text += String.fromCharCode.apply(null, bytes.subarray(i, Math.min(i + size, bytes.length)));
    }
    return { status: response.status, url: response.url, headers: outHeaders, body: btoa(text), error: "", length: bytes.length };
  } catch (err) {
    return { status: 0, url: job.url, headers: [], body: "", error: String(err && err.message ? err.message : err), length: 0 };
  }
})()`, nil
}

func resolveBrowser(setting string) (string, error) {
	setting = strings.TrimSpace(setting)
	if setting != "" && !strings.EqualFold(setting, "off") {
		if _, err := os.Stat(setting); err != nil {
			return "", fmt.Errorf("%w: 没有找到浏览器 %s", ErrBrowser, setting)
		}
		return setting, nil
	}
	for _, candidate := range browserCandidates() {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: 没有找到 Chrome 或 Edge. 安装其中一个, 或设置 RUTRACKER_BROWSER", ErrBrowser)
}

func browserCandidates() []string {
	programFiles := os.Getenv("ProgramFiles")
	programFilesX86 := os.Getenv("ProgramFiles(x86)")
	local := os.Getenv("LOCALAPPDATA")
	return []string{
		filepath.Join(programFiles, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(programFilesX86, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(local, "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(programFiles, "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(programFilesX86, "Microsoft", "Edge", "Application", "msedge.exe"),
	}
}

func chromeProxyServer(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("RUTRACKER_PROXY must be an http, https, or socks5 URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return raw, nil
	case "socks5", "socks5h":
		parsed.Scheme = "socks5"
		return parsed.String(), nil
	default:
		return "", fmt.Errorf("RUTRACKER_PROXY scheme %q is not supported", parsed.Scheme)
	}
}

func browserSeedCookie(name string) bool {
	switch strings.ToLower(name) {
	case "cf_clearance", "__cf_bm", "__cflb":
		return false
	default:
		return name != ""
	}
}

func skipBrowserHeader(name string) bool {
	switch strings.ToLower(name) {
	case "cookie", "host", "content-length", "user-agent", "accept-encoding", "connection":
		return true
	default:
		return false
	}
}

func originStart(target *url.URL) string {
	base := target.Scheme + "://" + target.Host + "/"
	host := strings.ToLower(target.Hostname())
	if strings.HasPrefix(host, "static.") {
		return base
	}
	for _, suffix := range []string{"rutracker.org", "rutracker.net", "rutracker.nl"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return base + "forum/index.php"
		}
	}
	return base
}

func cloudflareChallenge(status int, body []byte) bool {
	if status != http.StatusForbidden && status != http.StatusServiceUnavailable && status != http.StatusTooManyRequests {
		return false
	}
	text := strings.ToLower(string(body))
	return strings.Contains(text, "just a moment") ||
		strings.Contains(text, "challenge-platform") ||
		strings.Contains(text, "cf-browser-verification") ||
		strings.Contains(text, "请稍候")
}

func challengeTitle(title string) bool {
	low := strings.ToLower(strings.TrimSpace(title))
	if low == "" || low == "about:blank" {
		return true
	}
	return strings.Contains(low, "just a moment") || strings.Contains(title, "请稍候") || strings.Contains(low, "checking your browser")
}
