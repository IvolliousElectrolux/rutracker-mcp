package rutracker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DownloadFileName is the only name used on disk. The site's Content-Disposition
// value is never accepted as a path.
func DownloadFileName(topicID int) (string, error) {
	if topicID <= 0 {
		return "", fmt.Errorf("topic id must be positive")
	}
	return SanitizeFileName(strconv.Itoa(topicID) + ".torrent")
}

// SanitizeFileName reduces name to a single path segment that cannot escape a directory.
func SanitizeFileName(name string) (string, error) {
	name = strings.Map(func(r rune) rune {
		if r == 0 {
			return -1
		}
		return r
	}, name)
	name = strings.ReplaceAll(name, `\`, "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(name)
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." || name == ".." || strings.Contains(name, "..") {
		return "", ErrBadName
	}
	if strings.ContainsAny(name, `/\:*?"<>|`) {
		return "", ErrBadName
	}
	base := name
	if dot := strings.IndexByte(name, '.'); dot >= 0 {
		base = name[:dot]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return "", ErrBadName
	}
	if len(name) > 180 {
		return "", ErrBadName
	}
	return name, nil
}

// SaveTorrent writes body as {topicID}.torrent and returns the absolute path and sha256.
func SaveTorrent(dir string, topicID int, body []byte) (string, string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", "", fmt.Errorf("download directory is empty")
	}
	name, err := DownloadFileName(topicID)
	if err != nil {
		return "", "", err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(absDir, 0o700); err != nil {
		return "", "", err
	}
	dest, err := joinInside(absDir, name)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(body)
	tmp := dest + ".part"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return "", "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", "", err
	}
	return dest, hex.EncodeToString(sum[:]), nil
}

func joinInside(dir, name string) (string, error) {
	clean, err := SanitizeFileName(name)
	if err != nil {
		return "", err
	}
	if clean != name || strings.ContainsAny(name, `/\`) {
		return "", ErrBadName
	}
	dest := filepath.Join(dir, clean)
	rel, err := filepath.Rel(dir, dest)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", ErrBadName
	}
	return dest, nil
}
