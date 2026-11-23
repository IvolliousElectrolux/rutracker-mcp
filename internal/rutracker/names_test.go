package rutracker

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadFileNameUsesTopicID(t *testing.T) {
	name, err := DownloadFileName(6543210)
	if err != nil {
		t.Fatal(err)
	}
	if name != "6543210.torrent" {
		t.Fatalf("name = %s", name)
	}
	if _, err := DownloadFileName(0); err == nil {
		t.Fatal("expected topic id 0 to fail")
	}
	if _, err := DownloadFileName(-3); err == nil {
		t.Fatal("expected a negative topic id to fail")
	}
}

func TestSanitizeFileName(t *testing.T) {
	dir := t.TempDir()
	keep := []struct {
		in   string
		want string
	}{
		{
			in:   "ok.torrent",
			want: "ok.torrent",
		},
		{
			in:   "a\x00b.torrent",
			want: "ab.torrent",
		},
		{
			in:   `..\..\Windows\system.ini`,
			want: "system.ini",
		},
		{
			in:   `foo/bar.torrent`,
			want: "bar.torrent",
		},
		{
			in:   `C:\Windows\notepad.exe`,
			want: "notepad.exe",
		},
		{
			in:   `../../etc/passwd`,
			want: "passwd",
		},
		{
			in:   `....//....//windows/system32/cmd.exe`,
			want: "cmd.exe",
		},
	}
	for _, tc := range keep {
		name, want := tc.in, tc.want
		clean, err := SanitizeFileName(name)
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		if clean != want {
			t.Fatalf("%q cleaned to %q, want %q", name, clean, want)
		}
		dest, err := joinInside(dir, clean)
		if err != nil {
			t.Fatalf("join %q: %v", clean, err)
		}
		rel, err := filepath.Rel(dir, dest)
		if err != nil || rel != clean {
			t.Fatalf("%q escaped to %s", name, dest)
		}
	}

	for _, name := range []string{"con.torrent", "AUX.txt", "", ".", "..", "*"} {
		if _, err := SanitizeFileName(name); !errors.Is(err, ErrBadName) {
			t.Fatalf("%q should be rejected, err = %v", name, err)
		}
	}
	if _, err := joinInside(dir, `..\..\evil.torrent`); err == nil {
		t.Fatal("joinInside accepted a path")
	}
}

func TestSaveTorrentIgnoresSiteFileName(t *testing.T) {
	dir := t.TempDir()
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	path, sum, err := SaveTorrent(dir, 15, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if sum != want {
		t.Fatalf("sha256 = %s", sum)
	}
	if filepath.Base(path) != "15.torrent" {
		t.Fatalf("path = %s", path)
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("path is not absolute: %s", path)
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel != "15.torrent" {
		t.Fatalf("rel = %s, err = %v", rel, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("body = %q", data)
	}
	if _, err := os.Stat(path + ".part"); !os.IsNotExist(err) {
		t.Fatalf("partial file left behind: %v", err)
	}
}
