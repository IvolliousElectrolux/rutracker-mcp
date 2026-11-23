package rutracker

import (
	"bytes"
	"io"
	"net/url"
	"strings"
	"testing"
)

func TestEncodeFormWindows1251(t *testing.T) {
	got, err := encodeForm(url.Values{"login": {"вход"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "login=%E2%F5%EE%E4" {
		t.Fatalf("login form = %s", got)
	}
	if strings.Contains(strings.ToLower(got), "%d0") {
		t.Fatalf("encoded as UTF-8: %s", got)
	}
}

func TestDecodeWindows1251(t *testing.T) {
	raw := []byte{0xCC, 0xE0, 0xF2, 0xF0, 0xE8, 0xF6, 0xE0}
	decoded, err := io.ReadAll(decode1251(bytes.NewReader(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != "Матрица" {
		t.Fatalf("decoded %q", decoded)
	}
}

func TestEncodeFormRejectsUnmappableRunes(t *testing.T) {
	_, err := encodeForm(url.Values{"nm": {"测试"}})
	if err == nil {
		t.Fatal("expected windows-1251 error")
	}
}

func TestSearchQuerySortCodes(t *testing.T) {
	got, err := searchQuery("matrix", SearchOptions{ForumID: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got != "f=3&nm=matrix&o=10&s=2" {
		t.Fatalf("default query = %s", got)
	}

	got, err = searchQuery("тест", SearchOptions{Sort: "date", Order: "asc"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "nm=%F2%E5%F1%F2&o=1&s=1" {
		t.Fatalf("date query = %s", got)
	}

	cases := []struct {
		sort, order, field, direction string
	}{
		{"size", "asc", "7", "1"},
		{"downloads", "DESC", "4", "2"},
		{"Seeders", "desc", "10", "2"},
	}
	for _, tc := range cases {
		got, err = searchQuery("a", SearchOptions{Sort: tc.sort, Order: tc.order})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "o="+tc.field) || !strings.Contains(got, "s="+tc.direction) {
			t.Fatalf("sort %s %s => %s", tc.sort, tc.order, got)
		}
	}

	if _, err = searchQuery("a", SearchOptions{Sort: "title"}); err == nil {
		t.Fatal("expected invalid sort")
	}
	if _, err = searchQuery("a", SearchOptions{Order: "sideways"}); err == nil {
		t.Fatal("expected invalid order")
	}
}
