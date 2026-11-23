package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/IvolliousElectrolux/rutracker-mcp/internal/config"
	"github.com/IvolliousElectrolux/rutracker-mcp/internal/mcpapp"
	"github.com/IvolliousElectrolux/rutracker-mcp/internal/rutracker"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		exitErr(err)
	}
	fileOK, fileErr := rutracker.SessionFileHas(cfg.CookieFile)
	if fileErr != nil && cfg.Cookie == "" && !cfg.HasPassword() {
		fmt.Fprintf(os.Stderr, "rutracker-mcp: 无法读取 cookie 文件 %s: %v\n", cfg.CookieFile, fileErr)
		os.Exit(1)
	}
	if cfg.Cookie == "" && !cfg.HasPassword() && !fileOK {
		fmt.Fprintf(os.Stderr, "rutracker-mcp: 没有登录信息. 请设置 RUTRACKER_USERNAME 和 RUTRACKER_PASSWORD, 或设置 RUTRACKER_COOKIE, 或在 %s 中提供 bb_session\n", cfg.CookieFile)
		os.Exit(1)
	}

	client, err := rutracker.New(rutracker.Options{
		BaseURL:    cfg.BaseURL,
		Username:   cfg.Username,
		Password:   cfg.Password,
		Cookie:     cfg.Cookie,
		CookieFile: cfg.CookieFile,
		UserAgent:  cfg.UserAgent,
		Proxy:      cfg.Proxy,
		Browser:    cfg.Browser,
		Headless:   cfg.Headless,
	})
	if err != nil {
		exitErr(err)
	}
	if cfg.HasPassword() {
		rutracker.WarmOCR()
	}
	if err := mcpapp.Run(context.Background(), client, cfg.DownloadDir); err != nil && !cleanStop(err) {
		exitErr(err)
	}
}

func exitErr(err error) {
	fmt.Fprintf(os.Stderr, "rutracker-mcp: %v\n", err)
	os.Exit(1)
}

func cleanStop(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "eof") || strings.Contains(text, "closed")
}
