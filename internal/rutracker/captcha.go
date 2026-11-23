package rutracker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
)

const (
	maxCaptchaAttempts = 3
	maxCaptchaImage    = 2 << 20
)

type captchaChallenge struct {
	Image    string
	Sid      string
	Field    string
	Login    string
	Redirect string
	Code     string
}

// Solver reads a captcha image and returns the characters on it.
type Solver interface {
	Solve(ctx context.Context, image []byte) (string, error)
}

func (c *Client) login(ctx context.Context, base *url.URL) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.username == "" || c.password == "" {
		return ErrNoCredentials
	}
	var extra captchaChallenge
	haveExtra := false
	for attempt := 0; attempt <= maxCaptchaAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		form := url.Values{}
		form.Set("login_username", c.username)
		form.Set("login_password", c.password)
		if haveExtra {
			form.Set("login", extra.Login)
			form.Set("cap_sid", extra.Sid)
			form.Set(extra.Field, extra.Code)
			if extra.Redirect != "" {
				form.Set("redirect", extra.Redirect)
			}
		} else {
			form.Set("login", loginSubmit)
		}
		payload, session, err := c.postLogin(ctx, base, form)
		if err != nil {
			return err
		}
		if session {
			c.saveCookies(base)
			return nil
		}
		if attempt == maxCaptchaAttempts {
			return fmt.Errorf("%w: 连续 %d 次仍要求验证码", ErrCaptcha, maxCaptchaAttempts)
		}
		next, ok, err := parseCaptcha(payload, base)
		if err != nil {
			return err
		}
		if !ok {
			return classifyLogin(payload)
		}
		image, err := c.captchaImage(ctx, base, next.Image)
		if err != nil {
			return err
		}
		raw, err := c.recognize(ctx, image)
		if err != nil {
			return err
		}
		code := normalizeOCR(raw)
		if code == "" {
			return fmt.Errorf("%w: 识别结果为空", ErrCaptcha)
		}
		fmt.Fprintf(os.Stderr, "rutracker-mcp: 验证码识别为 %s\n", code)
		next.Code = code
		extra = next
		haveExtra = true
	}
	return fmt.Errorf("%w: 连续 %d 次仍要求验证码", ErrCaptcha, maxCaptchaAttempts)
}

func (c *Client) recognize(ctx context.Context, image []byte) (string, error) {
	if c.solver != nil {
		return c.solver.Solve(ctx, image)
	}
	return defaultOCR.Solve(ctx, image)
}

func (c *Client) postLogin(ctx context.Context, base *url.URL, form url.Values) ([]byte, bool, error) {
	body, err := encodeForm(form)
	if err != nil {
		return nil, false, err
	}
	target, err := endpointRaw(base, "login.php", "")
	if err != nil {
		return nil, false, err
	}
	req, err := c.newRequest(ctx, http.MethodPost, target, strings.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", base.String())

	loginHTTP := &http.Client{
		Jar:       c.jar,
		Timeout:   requestTimeout,
		Transport: c.http.Transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := loginHTTP.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, false, err
		}
		return nil, false, fmt.Errorf("%w: %s", ErrMirror, err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return nil, false, fmt.Errorf("%w: HTTP %d", ErrMirror, resp.StatusCode)
	}
	if sessionIssued(resp) {
		return nil, true, nil
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, false, err
	}
	return payload, false, nil
}

func (c *Client) captchaImage(ctx context.Context, base *url.URL, raw string) ([]byte, error) {
	img, err := url.Parse(raw)
	if err != nil || !allowedCaptchaURL(base, img) {
		return nil, fmt.Errorf("%w: 验证码图片地址不被接受", ErrCaptcha)
	}
	req, err := c.newRequest(ctx, http.MethodGet, img.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", base.String())
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", ErrMirror, err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("%w: HTTP %d", ErrMirror, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: 验证码图片 HTTP %d", ErrCaptcha, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCaptchaImage+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxCaptchaImage || !imageMagic(data) {
		return nil, fmt.Errorf("%w: 验证码图片大小异常", ErrCaptcha)
	}
	return data, nil
}

func parseCaptcha(payload []byte, base *url.URL) (captchaChallenge, bool, error) {
	if len(payload) == 0 {
		return captchaChallenge{}, false, nil
	}
	doc, err := goquery.NewDocumentFromReader(decode1251(bytes.NewReader(payload)))
	if err != nil {
		return captchaChallenge{}, false, ErrLogin
	}
	if !looksLikeCaptcha(doc) {
		return captchaChallenge{}, false, nil
	}
	sid := strings.TrimSpace(doc.Find(`input[name="cap_sid"]`).AttrOr("value", ""))
	field := ""
	doc.Find("input[name]").Each(func(_ int, sel *goquery.Selection) {
		name, _ := sel.Attr("name")
		if field == "" && strings.HasPrefix(name, "cap_code_") {
			field = name
		}
	})
	src := strings.TrimSpace(doc.Find(`img[src*="/captcha/"]`).AttrOr("src", ""))
	if !safeToken(sid) || !safeToken(field) || !strings.HasPrefix(field, "cap_code_") || src == "" {
		return captchaChallenge{}, false, fmt.Errorf("%w: 验证码页面缺少 cap_sid, cap_code 或图片", ErrCaptcha)
	}
	ref, err := base.Parse(src)
	if err != nil || !allowedCaptchaURL(base, ref) {
		return captchaChallenge{}, false, fmt.Errorf("%w: 验证码图片地址不被接受", ErrCaptcha)
	}
	redirect := strings.TrimSpace(doc.Find(`input[name="redirect"]`).AttrOr("value", ""))
	if !safeRedirect(redirect) {
		redirect = ""
	}
	return captchaChallenge{
		Image:    ref.String(),
		Sid:      sid,
		Field:    field,
		Login:    cleanLoginValue(doc.Find(`input[name="login"]`).AttrOr("value", "")),
		Redirect: redirect,
	}, true, nil
}

func looksLikeCaptcha(doc *goquery.Document) bool {
	return doc.Find(`input[name="cap_sid"], input[name^="cap_code_"], img[src*="/captcha/"]`).Length() > 0
}

func allowedCaptchaURL(base, img *url.URL) bool {
	if img == nil || !strings.EqualFold(img.Scheme, "https") || img.Host == "" || img.User != nil {
		return false
	}
	if !strings.Contains(img.EscapedPath(), "/captcha/") {
		return false
	}
	if base != nil && strings.EqualFold(img.Host, base.Host) {
		return true
	}
	host := strings.ToLower(img.Hostname())
	for _, suffix := range []string{"rutracker.org", "rutracker.net", "rutracker.cc", "rutracker.nl", "t-ru.org"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func safeToken(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

func safeRedirect(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > 200 || strings.Contains(value, "://") || strings.ContainsAny(value, "\r\n& ") {
		return false
	}
	return true
}

func cleanLoginValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > 32 || strings.ContainsAny(value, "\r\n\x00") {
		return loginSubmit
	}
	return value
}

func normalizeOCR(value string) string {
	var buf strings.Builder
	for _, r := range value {
		if r <= unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			buf.WriteRune(r)
		}
	}
	if buf.Len() == 0 || buf.Len() > 16 {
		return ""
	}
	return buf.String()
}

func imageMagic(data []byte) bool {
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return true
	}
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if len(data) >= len(png) && bytes.Equal(data[:len(png)], png) {
		return true
	}
	if bytes.HasPrefix(data, []byte("GIF87a")) || bytes.HasPrefix(data, []byte("GIF89a")) {
		return true
	}
	return len(data) >= 2 && data[0] == 'B' && data[1] == 'M'
}

func classifyLogin(payload []byte) error {
	if len(payload) == 0 {
		return ErrLogin
	}
	doc, err := goquery.NewDocumentFromReader(decode1251(bytes.NewReader(payload)))
	if err != nil {
		return ErrLogin
	}
	if looksLikeCaptcha(doc) {
		return ErrCaptcha
	}
	return ErrLogin
}
