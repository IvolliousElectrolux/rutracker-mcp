package rutracker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

const sessionCookieName = "bb_session"

type storedCookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// SessionFileHas reports whether path contains a non-empty bb_session.
// A missing file is not an error.
func SessionFileHas(path string) (bool, error) {
	if strings.TrimSpace(path) == "" {
		return false, nil
	}
	cookies, err := readCookieFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	for _, cookie := range cookies {
		if cookie.Name == sessionCookieName && cookie.Value != "" {
			return true, nil
		}
	}
	return false, nil
}

func parseCookieHeader(header string) []*http.Cookie {
	var cookies []*http.Cookie
	for _, part := range strings.Split(header, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || value == "" {
			continue
		}
		cookies = append(cookies, newCookie(name, value))
	}
	return cookies
}

func readCookieFile(path string) ([]*http.Cookie, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var stored []storedCookie
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("cookie file: %w", err)
	}
	cookies := make([]*http.Cookie, 0, len(stored))
	for _, item := range stored {
		name := strings.TrimSpace(item.Name)
		value := strings.TrimSpace(item.Value)
		if name == "" || value == "" {
			continue
		}
		cookies = append(cookies, newCookie(name, value))
	}
	return cookies, nil
}

func newCookie(name, value string) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
	}
}
