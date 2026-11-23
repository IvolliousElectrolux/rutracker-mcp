package rutracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/proxy"
)

const (
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	maxHTMLBytes     = 16 << 20
	maxTorrentBytes  = 32 << 20
	requestTimeout   = 3 * time.Minute
	loginSubmit      = "вход"
)

// Options configures a Client. Cookie, or both Username and Password, must be available before use.
type Options struct {
	BaseURL    string
	Username   string
	Password   string
	Cookie     string
	CookieFile string
	UserAgent  string
	Proxy      string
	Browser    string
	Headless   bool
}

// Client talks to one or two RuTracker forum mirrors.
type Client struct {
	bases      []*url.URL
	active     atomic.Int32
	http       *http.Client
	jar        http.CookieJar
	username   string
	password   string
	cookieFile string
	userAgent  string
	solver     Solver
	mu         sync.Mutex
}

// New builds a client and seeds its cookie jar. It does not log in yet.
func New(opts Options) (*Client, error) {
	bases, err := parseBases(opts.BaseURL)
	if err != nil {
		return nil, err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cookie jar: %w", err)
	}
	transport, err := proxyTransport(opts.Proxy)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(opts.Browser), "off") {
		browserTransport, err := newBrowserTransport(opts)
		if err != nil {
			return nil, err
		}
		transport = browserTransport
	}
	ua := strings.TrimSpace(opts.UserAgent)
	if ua == "" {
		ua = defaultUserAgent
	}
	client := &Client{
		bases:      bases,
		http:       &http.Client{Jar: jar, Timeout: requestTimeout, Transport: transport},
		jar:        jar,
		username:   opts.Username,
		password:   opts.Password,
		cookieFile: opts.CookieFile,
		userAgent:  ua,
	}
	if err := client.seed(opts.Cookie); err != nil {
		return nil, err
	}
	return client, nil
}

func parseBases(raw string) ([]*url.URL, error) {
	list := []string{
		"https://rutracker.org/forum/",
		"https://rutracker.net/forum/",
	}
	if strings.TrimSpace(raw) != "" {
		list = []string{strings.TrimSpace(raw)}
	}
	bases := make([]*url.URL, 0, len(list))
	for _, item := range list {
		parsed, err := url.Parse(item)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || !strings.EqualFold(parsed.Scheme, "https") {
			return nil, fmt.Errorf("%w: %s", ErrBaseURL, item)
		}
		parsed.Scheme = "https"
		if !strings.HasSuffix(parsed.Path, "/") {
			parsed.Path += "/"
		}
		parsed.RawQuery = ""
		parsed.Fragment = ""
		bases = append(bases, parsed)
	}
	return bases, nil
}

func proxyTransport(raw string) (http.RoundTripper, error) {
	base, ok := http.DefaultTransport.(*http.Transport)
	var transport *http.Transport
	if ok {
		transport = base.Clone()
	} else {
		transport = &http.Transport{}
	}
	transport.Proxy = nil
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return transport, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("RUTRACKER_PROXY must be an http, https, or socks5 URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		transport.Proxy = http.ProxyURL(parsed)
		return transport, nil
	case "socks5", "socks5h":
		dialer, err := proxy.FromURL(parsed, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("RUTRACKER_PROXY: %w", err)
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if withContext, ok := dialer.(proxy.ContextDialer); ok {
				return withContext.DialContext(ctx, network, addr)
			}
			return dialer.Dial(network, addr)
		}
		return transport, nil
	default:
		return nil, fmt.Errorf("RUTRACKER_PROXY scheme %q is not supported", parsed.Scheme)
	}
}

func (c *Client) seed(header string) error {
	if strings.TrimSpace(header) != "" {
		c.store(parseCookieHeader(header))
		return nil
	}
	if c.cookieFile == "" {
		return nil
	}
	cookies, err := readCookieFile(c.cookieFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	c.store(cookies)
	return nil
}

func (c *Client) store(cookies []*http.Cookie) {
	if len(cookies) == 0 || c.jar == nil {
		return
	}
	for _, base := range c.bases {
		c.jar.SetCookies(base, cookies)
	}
}

func (c *Client) try(ctx context.Context, fn func(*url.URL) error) error {
	count := len(c.bases)
	start := int(c.active.Load())
	var last error
	for i := 0; i < count; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		idx := (start + i) % count
		err := fn(c.bases[idx])
		if err == nil {
			c.active.Store(int32(idx))
			return nil
		}
		if errors.Is(err, ErrMirror) {
			last = err
			continue
		}
		return err
	}
	if last == nil {
		return ErrMirror
	}
	return last
}

func (c *Client) authed(ctx context.Context, base *url.URL, op func() error) error {
	if err := c.ensure(ctx, base); err != nil {
		return err
	}
	err := op()
	if !errors.Is(err, ErrSession) {
		return err
	}
	if c.username == "" || c.password == "" {
		return err
	}
	if err := c.login(ctx, base); err != nil {
		return err
	}
	return op()
}

func (c *Client) ensure(ctx context.Context, base *url.URL) error {
	if c.sessionValue(base) != "" {
		return nil
	}
	return c.login(ctx, base)
}

func sessionIssued(resp *http.Response) bool {
	for _, cookie := range resp.Cookies() {
		if cookie.Name == sessionCookieName && cookie.Value != "" {
			return true
		}
	}
	return false
}

func (c *Client) sessionValue(base *url.URL) string {
	if c.jar == nil {
		return ""
	}
	for _, cookie := range c.jar.Cookies(base) {
		if cookie.Name == sessionCookieName && cookie.Value != "" {
			return cookie.Value
		}
	}
	return ""
}

func (c *Client) saveCookies(base *url.URL) {
	if c.cookieFile == "" || c.jar == nil {
		return
	}
	stored := make([]storedCookie, 0)
	for _, cookie := range c.jar.Cookies(base) {
		if cookie.Name == "" || cookie.Value == "" {
			continue
		}
		stored = append(stored, storedCookie{Name: cookie.Name, Value: cookie.Value})
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.cookieFile), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "rutracker-mcp: could not create cookie directory: %v\n", err)
		return
	}
	if err := os.WriteFile(c.cookieFile, append(data, '\n'), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "rutracker-mcp: could not save cookies: %v\n", err)
	}
}

func (c *Client) newRequest(ctx context.Context, method, target string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	return req, nil
}

func (c *Client) roundTrip(req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", ErrMirror, err.Error())
	}
	if resp.StatusCode >= 500 {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: HTTP %d from %s", ErrMirror, resp.StatusCode, req.URL.Host)
	}
	return resp, nil
}

func endpointRaw(base *url.URL, page, rawQuery string) (string, error) {
	target, err := url.JoinPath(base.String(), page)
	if err != nil {
		return "", err
	}
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	return target, nil
}

func topicURL(base *url.URL, topicID int) string {
	target, err := endpointRaw(base, "viewtopic.php", "t="+strconv.Itoa(topicID))
	if err != nil {
		return ""
	}
	return target
}

func loginRedirect(resp *http.Response) bool {
	if resp == nil || resp.Request == nil || resp.Request.URL == nil {
		return false
	}
	return strings.Contains(strings.ToLower(resp.Request.URL.EscapedPath()), "login.php")
}
