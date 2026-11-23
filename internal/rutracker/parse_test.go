package rutracker

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestParseSearchFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/search.html")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	hits := parseSearch(doc)
	if len(hits) != 2 {
		t.Fatalf("hits = %d", len(hits))
	}
	first := hits[0]
	if first.TopicID != 1001 || first.Title != "Example Movie" || first.ForumID != 22 || first.Forum != "Films" {
		t.Fatalf("first = %+v", first)
	}
	if first.SizeBytes != 4096 || first.Seeders != 8 || first.Leechers != 2 || first.Downloads != 77 {
		t.Fatalf("first stats = %+v", first)
	}
	second := hits[1]
	if second.TopicID != 1002 || second.Title != "Second" || second.ForumID != 9 || second.SizeBytes != 8 {
		t.Fatalf("second = %+v", second)
	}
	if second.Seeders != 1 || second.Downloads != 3 {
		t.Fatalf("second stats = %+v", second)
	}
}

func TestParseTopicAndFiles(t *testing.T) {
	topicHTML := `<html><body>
<div class="nav"><a href="index.php">Home</a> <a href="viewforum.php?f=22">Films</a></div>
<h1 class="maintitle"><a id="topic-title" href="viewtopic.php?t=1001">Example Movie</a></h1>
<a class="magnet-link" href="magnet:?xt=urn:btih:abcdef0123456789abcdef0123456789abcdef01&amp;xl=4096">magnet</a>
<span id="tor-size-humn" title="4096">4 KB</span>
<span class="seed"><b>8</b></span>
<span class="leech"><b>2</b></span>
<p class="dl_links_count">downloads <b>77</b></p>
</body></html>`
	doc := mustDoc(t, topicHTML)
	info, err := parseTopic(doc, 1001)
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "Example Movie" || info.Forum != "Films" || info.SizeBytes != 4096 {
		t.Fatalf("topic = %+v", info)
	}
	if info.Seeders != 8 || info.Leechers != 2 || info.Downloads != 77 {
		t.Fatalf("stats = %+v", info)
	}
	const wantHash = "ABCDEF0123456789ABCDEF0123456789ABCDEF01"
	if info.InfoHash != wantHash || info.Magnet == "" {
		t.Fatalf("magnet = %s hash = %s", info.Magnet, info.InfoHash)
	}

	fallback := mustDoc(t, `<html><body><h1 class="maintitle">Only Title</h1><a href="magnet:?xt=urn:btih:aabb">x</a></body></html>`)
	info, err = parseTopic(fallback, 5)
	if err != nil {
		t.Fatal(err)
	}
	if info.InfoHash != "AABB" {
		t.Fatalf("fallback hash = %s", info.InfoHash)
	}

	files := mustDoc(t, `<ul class="ftree">
<li><div><b>Season</b></div>
<ul>
<li><div><b>e01.mkv</b><i>1500</i></div></li>
<li><div><b>e02.mkv</b><i>1 600</i></div></li>
</ul></li>
<li><div><b>readme.txt</b><i>12</i></div></li>
</ul>`)
	list, err := parseFiles(files, 1001)
	if err != nil {
		t.Fatal(err)
	}
	if list.FileCount != 3 || list.TotalSizeBytes != 3112 {
		t.Fatalf("list = %+v", list)
	}
	if list.Files[0].Path != "Season/e01.mkv" || list.Files[0].SizeBytes != 1500 {
		t.Fatalf("file0 = %+v", list.Files[0])
	}
	if list.Files[1].Path != "Season/e02.mkv" || list.Files[1].SizeBytes != 1600 {
		t.Fatalf("file1 = %+v", list.Files[1])
	}
	if list.Files[2].Path != "readme.txt" || list.Files[2].SizeBytes != 12 {
		t.Fatalf("file2 = %+v", list.Files[2])
	}

	empty, err := parseFiles(mustDoc(t, `<ul class="ftree"></ul>`), 1)
	if !errors.Is(err, ErrNotFound) || empty.FileCount != 0 {
		t.Fatalf("empty = %+v, %v", empty, err)
	}
	if _, err := parseFiles(mustDoc(t, `<html></html>`), 1); !errors.Is(err, ErrSession) {
		t.Fatalf("missing tree: %v", err)
	}
}

func mustDoc(t *testing.T, html string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(stringsNew(html))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func stringsNew(s string) *bytes.Reader {
	return bytes.NewReader([]byte(s))
}
