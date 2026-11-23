// Package mcpapp exposes the RuTracker tools over stdio MCP.
package mcpapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/IvolliousElectrolux/rutracker-mcp/internal/rutracker"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Client is the site API the tools call.
type Client interface {
	Search(ctx context.Context, query string, opt rutracker.SearchOptions) ([]rutracker.SearchHit, error)
	Topic(ctx context.Context, topicID int) (rutracker.Topic, error)
	Files(ctx context.Context, topicID int) (rutracker.FileList, error)
	Download(ctx context.Context, topicID int) ([]byte, error)
}

var _ Client = (*rutracker.Client)(nil)

type service struct {
	client Client
	dir    string
}

type searchIn struct {
	Query   string `json:"query" jsonschema:"search keywords"`
	ForumID int    `json:"forumId,omitempty" jsonschema:"forum id or 0 for every forum"`
	Sort    string `json:"sort,omitempty" jsonschema:"seeders size date or downloads"`
	Order   string `json:"order,omitempty" jsonschema:"desc or asc"`
	Limit   int    `json:"limit,omitempty" jsonschema:"max rows or 0 for the whole page"`
}

type searchHit struct {
	TopicID   int    `json:"topicId"`
	Title     string `json:"title"`
	ForumID   int    `json:"forumId"`
	Forum     string `json:"forum"`
	SizeBytes int64  `json:"sizeBytes"`
	Seeders   int    `json:"seeders"`
	Leechers  int    `json:"leechers"`
	Downloads int    `json:"downloads"`
	URL       string `json:"url"`
}

type searchOut struct {
	Results []searchHit `json:"results"`
}

type topicIn struct {
	TopicID int `json:"topicId" jsonschema:"RuTracker topic id"`
}

type topicOut struct {
	TopicID   int    `json:"topicId"`
	Title     string `json:"title"`
	Forum     string `json:"forum"`
	SizeBytes int64  `json:"sizeBytes"`
	Seeders   int    `json:"seeders"`
	Leechers  int    `json:"leechers"`
	Downloads int    `json:"downloads"`
	Magnet    string `json:"magnet"`
	InfoHash  string `json:"infoHash"`
	URL       string `json:"url"`
}

type fileOut struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
}

type filesOut struct {
	TopicID        int       `json:"topicId"`
	FileCount      int       `json:"fileCount"`
	TotalSizeBytes int64     `json:"totalSizeBytes"`
	Files          []fileOut `json:"files"`
}

type magnetOut struct {
	TopicID  int    `json:"topicId"`
	Magnet   string `json:"magnet"`
	InfoHash string `json:"infoHash"`
}

type downloadOut struct {
	TopicID   int    `json:"topicId"`
	Path      string `json:"path"`
	SizeBytes int    `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
}

// Register adds the five RuTracker tools.
func Register(server *mcp.Server, client Client, downloadDir string) {
	svc := &service{client: client, dir: downloadDir}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "rutracker_search",
		Description: "按关键词搜索 RuTracker. sort 取 seeders, size, date, downloads. order 取 desc 或 asc. forumId 为 0 时不限论坛.",
	}, svc.search)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "rutracker_topic",
		Description: "读取主题标题, 大小, 做种数, 下载数, magnet 和 info-hash.",
	}, svc.topic)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "rutracker_files",
		Description: "不下载种子, 列出其中每个文件的路径和精确字节数.",
	}, svc.files)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "rutracker_magnet",
		Description: "只返回主题的 magnet 和 info-hash.",
	}, svc.magnet)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "rutracker_download",
		Description: "把 .torrent 保存到本机目录, 文件名固定为 {topicId}.torrent, 只返回绝对路径, 字节数和 sha256.",
	}, svc.download)
}

// Run serves MCP on stdin and stdout. Logs go to stderr.
func Run(ctx context.Context, client Client, downloadDir string) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "rutracker-mcp",
		Version: "0.1.0",
	}, &mcp.ServerOptions{
		Logger:       logger,
		Instructions: "搜索 RuTracker, 查看主题, 文件列表和 magnet, 并把 .torrent 保存到本机目录. 下载结果只有绝对路径, 字节数和 sha256, 不包含种子正文.",
	})
	Register(server, client, downloadDir)
	return server.Run(ctx, &mcp.StdioTransport{})
}

func (s *service) search(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, searchOut, error) {
	hits, err := s.client.Search(ctx, in.Query, rutracker.SearchOptions{
		ForumID: in.ForumID,
		Sort:    in.Sort,
		Order:   in.Order,
		Limit:   in.Limit,
	})
	if err != nil {
		return nil, searchOut{}, explain(err)
	}
	out := searchOut{Results: make([]searchHit, 0, len(hits))}
	for _, hit := range hits {
		out.Results = append(out.Results, searchHit{
			TopicID:   hit.TopicID,
			Title:     hit.Title,
			ForumID:   hit.ForumID,
			Forum:     hit.Forum,
			SizeBytes: hit.SizeBytes,
			Seeders:   hit.Seeders,
			Leechers:  hit.Leechers,
			Downloads: hit.Downloads,
			URL:       hit.URL,
		})
	}
	return nil, out, nil
}

func (s *service) topic(ctx context.Context, _ *mcp.CallToolRequest, in topicIn) (*mcp.CallToolResult, topicOut, error) {
	info, err := s.client.Topic(ctx, in.TopicID)
	if err != nil {
		return nil, topicOut{}, explain(err)
	}
	return nil, topicOut{
		TopicID:   info.TopicID,
		Title:     info.Title,
		Forum:     info.Forum,
		SizeBytes: info.SizeBytes,
		Seeders:   info.Seeders,
		Leechers:  info.Leechers,
		Downloads: info.Downloads,
		Magnet:    info.Magnet,
		InfoHash:  info.InfoHash,
		URL:       info.URL,
	}, nil
}

func (s *service) files(ctx context.Context, _ *mcp.CallToolRequest, in topicIn) (*mcp.CallToolResult, filesOut, error) {
	list, err := s.client.Files(ctx, in.TopicID)
	if err != nil {
		return nil, filesOut{}, explain(err)
	}
	out := filesOut{
		TopicID:        list.TopicID,
		FileCount:      list.FileCount,
		TotalSizeBytes: list.TotalSizeBytes,
		Files:          make([]fileOut, 0, len(list.Files)),
	}
	for _, file := range list.Files {
		out.Files = append(out.Files, fileOut{Path: file.Path, SizeBytes: file.SizeBytes})
	}
	return nil, out, nil
}

func (s *service) magnet(ctx context.Context, _ *mcp.CallToolRequest, in topicIn) (*mcp.CallToolResult, magnetOut, error) {
	info, err := s.client.Topic(ctx, in.TopicID)
	if err != nil {
		return nil, magnetOut{}, explain(err)
	}
	if info.Magnet == "" {
		return nil, magnetOut{}, fmt.Errorf("这个主题没有 magnet")
	}
	return nil, magnetOut{TopicID: info.TopicID, Magnet: info.Magnet, InfoHash: info.InfoHash}, nil
}

func (s *service) download(ctx context.Context, _ *mcp.CallToolRequest, in topicIn) (*mcp.CallToolResult, downloadOut, error) {
	body, err := s.client.Download(ctx, in.TopicID)
	if err != nil {
		return nil, downloadOut{}, explain(err)
	}
	path, sum, err := rutracker.SaveTorrent(s.dir, in.TopicID, body)
	if err != nil {
		return nil, downloadOut{}, explain(err)
	}
	return nil, downloadOut{
		TopicID:   in.TopicID,
		Path:      path,
		SizeBytes: len(body),
		SHA256:    sum,
	}, nil
}

func explain(err error) error {
	switch {
	case errors.Is(err, rutracker.ErrCaptcha):
		detail := strings.TrimPrefix(err.Error(), rutracker.ErrCaptcha.Error())
		detail = strings.TrimPrefix(detail, ": ")
		if detail == "" {
			return fmt.Errorf("RuTracker 要求验证码. 请确认 rutracker-ocr.exe 与主程序在同一目录, 或改用 RUTRACKER_COOKIE")
		}
		return fmt.Errorf("RuTracker 验证码没有通过: %s", detail)
	case errors.Is(err, rutracker.ErrNoCredentials):
		return fmt.Errorf("没有可用的登录信息, 请设置账号密码或 RUTRACKER_COOKIE")
	case errors.Is(err, rutracker.ErrSession):
		return fmt.Errorf("RuTracker 会话失效, 请检查账号密码, 或更新 RUTRACKER_COOKIE")
	case errors.Is(err, rutracker.ErrLogin):
		return fmt.Errorf("RuTracker 拒绝了这次登录")
	case errors.Is(err, rutracker.ErrNotFound):
		return fmt.Errorf("没有找到这个主题, 或文件列表为空")
	case errors.Is(err, rutracker.ErrTooLarge):
		return fmt.Errorf("种子大于 32MB, 已拒绝保存")
	case errors.Is(err, rutracker.ErrMirror):
		return fmt.Errorf("RuTracker 镜像暂时不可用: %s", err.Error())
	default:
		return err
	}
}
