package rutracker

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// SearchHit is one row from the tracker results table.
type SearchHit struct {
	TopicID   int
	Title     string
	ForumID   int
	Forum     string
	SizeBytes int64
	Seeders   int
	Leechers  int
	Downloads int
	URL       string
}

// Topic is the detail page for one torrent.
type Topic struct {
	TopicID   int
	Title     string
	Forum     string
	SizeBytes int64
	Seeders   int
	Leechers  int
	Downloads int
	Magnet    string
	InfoHash  string
	URL       string
}

// FileEntry is one file inside a torrent, with its exact size.
type FileEntry struct {
	Path      string
	SizeBytes int64
}

// FileList is the file tree from viewtorrent.php.
type FileList struct {
	TopicID        int
	FileCount      int
	TotalSizeBytes int64
	Files          []FileEntry
}

func parseSearch(doc *goquery.Document) []SearchHit {
	hits := make([]SearchHit, 0)
	doc.Find("#tor-tbl tbody tr").Each(func(_ int, row *goquery.Selection) {
		hit, ok := parseRow(row)
		if ok {
			hits = append(hits, hit)
		}
	})
	return hits
}

func parseRow(row *goquery.Selection) (SearchHit, bool) {
	link := row.Find("a.tLink").First()
	if link.Length() == 0 {
		return SearchHit{}, false
	}
	topicID := int(digits(link.AttrOr("data-topic_id", "")))
	if topicID == 0 {
		topicID = hrefQueryInt(link.AttrOr("href", ""), "t")
	}
	if topicID == 0 {
		return SearchHit{}, false
	}
	forum := row.Find("a.gen.f").First()
	sizeCell := row.Find("td.tor-size").First()
	downloadCell := row.Find("td.number-format").First()
	seed := row.Find("b.seedmed").First()
	if seed.Length() == 0 {
		seed = row.Find("td.seedmed").First()
	}
	downloadText := downloadCell.AttrOr("data-ts_text", "")
	if downloadText == "" {
		downloadText = downloadCell.Text()
	}
	return SearchHit{
		TopicID:   topicID,
		Title:     clean(link.Text()),
		ForumID:   hrefQueryInt(forum.AttrOr("href", ""), "f"),
		Forum:     clean(forum.Text()),
		SizeBytes: digits(sizeCell.AttrOr("data-ts_text", "")),
		Seeders:   int(digits(seed.Text())),
		Leechers:  int(digits(row.Find("td.leechmed, b.leechmed").First().Text())),
		Downloads: int(digits(downloadText)),
	}, true
}

func parseTopic(doc *goquery.Document, topicID int) (Topic, error) {
	title := clean(doc.Find("a#topic-title, h1.maintitle").First().Text())
	if title == "" {
		if doc.Find("input[name='login_username'], form[action*='login.php']").Length() > 0 {
			return Topic{}, ErrSession
		}
		return Topic{}, fmt.Errorf("topic page has no title")
	}
	magnet := magnetHref(doc)
	info := Topic{
		TopicID:   topicID,
		Title:     title,
		Forum:     clean(doc.Find(".nav a").Last().Text()),
		SizeBytes: topicSize(doc, magnet),
		Seeders:   int(digits(doc.Find("span.seed b, b.seedmed, .seedmed").First().Text())),
		Leechers:  int(digits(doc.Find("span.leech b, b.leechmed, .leechmed").First().Text())),
		Downloads: downloadCount(doc),
		Magnet:    magnet,
		InfoHash:  infoHash(magnet),
	}
	return info, nil
}

func parseFiles(doc *goquery.Document, topicID int) (FileList, error) {
	root := doc.Find("ul.ftree").First()
	if root.Length() == 0 {
		return FileList{}, ErrSession
	}
	files := make([]FileEntry, 0)
	walkFiles(root, "", &files)
	if len(files) == 0 {
		return FileList{}, ErrNotFound
	}
	var total int64
	for _, file := range files {
		total += file.SizeBytes
	}
	return FileList{
		TopicID:        topicID,
		FileCount:      len(files),
		TotalSizeBytes: total,
		Files:          files,
	}, nil
}

func walkFiles(list *goquery.Selection, prefix string, out *[]FileEntry) {
	list.ChildrenFiltered("li").Each(func(_ int, item *goquery.Selection) {
		div := item.ChildrenFiltered("div").First()
		name := clean(div.ChildrenFiltered("b").First().Text())
		nested := item.ChildrenFiltered("ul").First()
		if nested.Length() > 0 {
			walkFiles(nested, joinTorrentPath(prefix, name), out)
			return
		}
		*out = append(*out, FileEntry{
			Path:      joinTorrentPath(prefix, name),
			SizeBytes: digits(div.ChildrenFiltered("i").First().Text()),
		})
	})
}

func joinTorrentPath(prefix, name string) string {
	switch {
	case prefix == "":
		return name
	case name == "":
		return prefix
	default:
		return prefix + "/" + name
	}
}

func magnetHref(doc *goquery.Document) string {
	if href, ok := doc.Find("a.magnet-link").First().Attr("href"); ok && href != "" {
		return href
	}
	if href, ok := doc.Find("a[href^='magnet:']").First().Attr("href"); ok {
		return href
	}
	return ""
}

func topicSize(doc *goquery.Document, magnet string) int64 {
	span := doc.Find("#tor-size-humn, span.size-humn").First()
	for _, attr := range []string{"title", "data-ts_text"} {
		if n := digits(span.AttrOr(attr, "")); n > 0 {
			return n
		}
	}
	return magnetLength(magnet)
}

func magnetLength(magnet string) int64 {
	parsed, err := url.Parse(magnet)
	if err != nil {
		return 0
	}
	return digits(parsed.Query().Get("xl"))
}

func downloadCount(doc *goquery.Document) int {
	node := doc.Find("p.dl_links_count, span.dl-count").First()
	text := node.Find("b").First().Text()
	if strings.TrimSpace(text) == "" {
		text = node.Text()
	}
	return int(digits(text))
}

func infoHash(magnet string) string {
	parsed, err := url.Parse(magnet)
	if err != nil {
		return ""
	}
	const prefix = "urn:btih:"
	for _, xt := range parsed.Query()["xt"] {
		if len(xt) > len(prefix) && strings.EqualFold(xt[:len(prefix)], prefix) {
			return strings.ToUpper(xt[len(prefix):])
		}
	}
	return ""
}

func clean(text string) string {
	text = strings.ReplaceAll(text, "\u00a0", " ")
	text = strings.ReplaceAll(text, "\u200b", "")
	return strings.Join(strings.Fields(text), " ")
}

func digits(text string) int64 {
	var buf strings.Builder
	for _, r := range text {
		if r >= '0' && r <= '9' {
			buf.WriteRune(r)
		}
	}
	if buf.Len() == 0 {
		return 0
	}
	n, err := strconv.ParseInt(buf.String(), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func hrefQueryInt(href, key string) int {
	parsed, err := url.Parse(href)
	if err != nil {
		return 0
	}
	return int(digits(parsed.Query().Get(key)))
}
