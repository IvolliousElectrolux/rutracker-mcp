package rutracker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// SearchOptions selects forum, sort, and a client-side row cap.
// Limit 0 keeps every row on the returned page.
type SearchOptions struct {
	ForumID int
	Sort    string
	Order   string
	Limit   int
}

// Search runs a tracker.php query.
func (c *Client) Search(ctx context.Context, query string, opt SearchOptions) ([]SearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is empty")
	}
	if opt.ForumID < 0 {
		return nil, fmt.Errorf("forumId must be >= 0")
	}
	if opt.Limit < 0 {
		return nil, fmt.Errorf("limit must be >= 0")
	}
	rawQuery, err := searchQuery(query, opt)
	if err != nil {
		return nil, err
	}
	var hits []SearchHit
	err = c.try(ctx, func(base *url.URL) error {
		return c.authed(ctx, base, func() error {
			doc, err := c.getDoc(ctx, base, "tracker.php", rawQuery, true)
			if err != nil {
				return err
			}
			parsed := parseSearch(doc)
			for i := range parsed {
				parsed[i].URL = topicURL(base, parsed[i].TopicID)
			}
			if opt.Limit > 0 && len(parsed) > opt.Limit {
				parsed = parsed[:opt.Limit]
			}
			hits = parsed
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if hits == nil {
		hits = []SearchHit{}
	}
	return hits, nil
}

// Topic reads one viewtopic.php page.
func (c *Client) Topic(ctx context.Context, topicID int) (Topic, error) {
	if topicID <= 0 {
		return Topic{}, fmt.Errorf("topic id must be positive")
	}
	raw := "t=" + strconv.Itoa(topicID)
	var info Topic
	err := c.try(ctx, func(base *url.URL) error {
		return c.authed(ctx, base, func() error {
			doc, err := c.getDoc(ctx, base, "viewtopic.php", raw, true)
			if err != nil {
				return err
			}
			parsed, err := parseTopic(doc, topicID)
			if err != nil {
				return err
			}
			parsed.URL = topicURL(base, topicID)
			info = parsed
			return nil
		})
	})
	return info, err
}

// Files lists a torrent's files without downloading the .torrent.
func (c *Client) Files(ctx context.Context, topicID int) (FileList, error) {
	if topicID <= 0 {
		return FileList{}, fmt.Errorf("topic id must be positive")
	}
	var list FileList
	err := c.try(ctx, func(base *url.URL) error {
		return c.authed(ctx, base, func() error {
			form := url.Values{}
			form.Set("t", strconv.Itoa(topicID))
			resp, err := c.postForm(ctx, base, "viewtorrent.php", form, topicURL(base, topicID), true)
			if err != nil {
				return err
			}
			doc, err := readDoc(resp, false)
			if err != nil {
				return err
			}
			parsed, err := parseFiles(doc, topicID)
			if err != nil {
				return err
			}
			list = parsed
			return nil
		})
	})
	return list, err
}

// Download fetches the .torrent body. Callers must not use the site's file name as a path.
func (c *Client) Download(ctx context.Context, topicID int) ([]byte, error) {
	if topicID <= 0 {
		return nil, fmt.Errorf("topic id must be positive")
	}
	var body []byte
	err := c.try(ctx, func(base *url.URL) error {
		return c.authed(ctx, base, func() error {
			payload, err := c.download(ctx, base, topicID)
			if err != nil {
				return err
			}
			body = payload
			return nil
		})
	})
	return body, err
}

func (c *Client) getDoc(ctx context.Context, base *url.URL, page, rawQuery string, cp1251 bool) (*goquery.Document, error) {
	target, err := endpointRaw(base, page, rawQuery)
	if err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.roundTrip(req)
	if err != nil {
		return nil, err
	}
	return readDoc(resp, cp1251)
}

func (c *Client) postForm(ctx context.Context, base *url.URL, page string, form url.Values, referer string, ajax bool) (*http.Response, error) {
	raw, err := encodeForm(form)
	if err != nil {
		return nil, err
	}
	target, err := endpointRaw(base, page, "")
	if err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, http.MethodPost, target, strings.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if ajax {
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	return c.roundTrip(req)
}

func (c *Client) download(ctx context.Context, base *url.URL, topicID int) ([]byte, error) {
	id := strconv.Itoa(topicID)
	target, err := endpointRaw(base, "dl.php", "t="+id)
	if err != nil {
		return nil, err
	}
	req, err := c.newRequest(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", topicURL(base, topicID))
	resp, err := c.roundTrip(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if loginRedirect(resp) {
		return nil, ErrSession
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTorrentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read torrent: %w", err)
	}
	if int64(len(data)) > maxTorrentBytes {
		return nil, ErrTooLarge
	}
	if len(data) == 0 || data[0] != 'd' {
		return nil, ErrSession
	}
	return data, nil
}

func readDoc(resp *http.Response, cp1251 bool) (*goquery.Document, error) {
	defer resp.Body.Close()
	if loginRedirect(resp) {
		return nil, ErrSession
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var reader io.Reader = io.LimitReader(resp.Body, maxHTMLBytes)
	if cp1251 {
		reader = decode1251(reader)
	}
	doc, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}
	return doc, nil
}

func searchQuery(query string, opt SearchOptions) (string, error) {
	field, err := sortFieldCode(opt.Sort)
	if err != nil {
		return "", err
	}
	direction, err := sortOrderCode(opt.Order)
	if err != nil {
		return "", err
	}
	values := url.Values{}
	values.Set("nm", query)
	values.Set("o", field)
	values.Set("s", direction)
	if opt.ForumID > 0 {
		values.Set("f", strconv.Itoa(opt.ForumID))
	}
	return encodeForm(values)
}

func sortFieldCode(field string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "", "seeders":
		return "10", nil
	case "size":
		return "7", nil
	case "date":
		return "1", nil
	case "downloads":
		return "4", nil
	default:
		return "", fmt.Errorf("sort must be seeders, size, date, or downloads")
	}
}

func sortOrderCode(order string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(order)) {
	case "", "desc":
		return "2", nil
	case "asc":
		return "1", nil
	default:
		return "", fmt.Errorf("order must be desc or asc")
	}
}
