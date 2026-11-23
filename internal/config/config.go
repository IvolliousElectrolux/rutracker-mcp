// Package config reads rutracker-mcp settings from the environment.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// Config is the process configuration. Secrets stay in memory and are not logged.
type Config struct {
	Username    string
	Password    string
	Cookie      string
	CookieFile  string
	BaseURL     string
	UserAgent   string
	Proxy       string
	DownloadDir string
}

// Load reads RUTRACKER_* variables. An empty base URL means mirror failover.
func Load() (Config, error) {
	home, homeErr := os.UserHomeDir()
	cfg := Config{
		Username:  strings.TrimSpace(os.Getenv("RUTRACKER_USERNAME")),
		Password:  os.Getenv("RUTRACKER_PASSWORD"),
		Cookie:    strings.TrimSpace(os.Getenv("RUTRACKER_COOKIE")),
		UserAgent: strings.TrimSpace(os.Getenv("RUTRACKER_USER_AGENT")),
		BaseURL:   strings.TrimSpace(os.Getenv("RUTRACKER_BASE_URL")),
		Proxy:     strings.TrimSpace(os.Getenv("RUTRACKER_PROXY")),
	}

	cfg.CookieFile = strings.TrimSpace(os.Getenv("RUTRACKER_COOKIE_FILE"))
	if cfg.CookieFile == "" {
		if homeErr != nil {
			return Config{}, fmt.Errorf("RUTRACKER_COOKIE_FILE is empty and the home directory is unavailable")
		}
		cfg.CookieFile = filepath.Join(home, ".rutracker-mcp", "cookies.json")
	}

	cfg.DownloadDir = strings.TrimSpace(os.Getenv("RUTRACKER_DOWNLOAD_DIR"))
	if cfg.DownloadDir == "" {
		if homeErr != nil {
			return Config{}, fmt.Errorf("RUTRACKER_DOWNLOAD_DIR is empty and the home directory is unavailable")
		}
		cfg.DownloadDir = filepath.Join(home, ".rutracker-mcp", "torrents")
	}

	if cfg.BaseURL != "" {
		u, err := url.Parse(cfg.BaseURL)
		if err != nil || u.Scheme == "" || u.Host == "" || !strings.EqualFold(u.Scheme, "https") {
			return Config{}, fmt.Errorf("RUTRACKER_BASE_URL must be an https URL with a host")
		}
	}
	if err := checkProxy(cfg.Proxy); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// HasPassword reports whether both login fields are set.
func (c Config) HasPassword() bool {
	return c.Username != "" && c.Password != ""
}

func checkProxy(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("RUTRACKER_PROXY must be an http, https, or socks5 URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
		return nil
	default:
		return fmt.Errorf("RUTRACKER_PROXY scheme %q is not supported", u.Scheme)
	}
}
