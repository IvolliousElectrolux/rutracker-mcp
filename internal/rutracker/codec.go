package rutracker

import (
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// encodeForm percent-encodes values as windows-1251, which is what the site expects.
func encodeForm(values url.Values) (string, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var buf strings.Builder
	for _, key := range keys {
		for _, value := range values[key] {
			encoded, err := encode1251(value)
			if err != nil {
				return "", fmt.Errorf("encode %s as windows-1251: %w", key, err)
			}
			if buf.Len() > 0 {
				buf.WriteByte('&')
			}
			buf.WriteString(url.QueryEscape(key))
			buf.WriteByte('=')
			buf.WriteString(url.QueryEscape(encoded))
		}
	}
	return buf.String(), nil
}

func encode1251(value string) (string, error) {
	encoded, _, err := transform.String(charmap.Windows1251.NewEncoder(), value)
	if err != nil {
		return "", err
	}
	return encoded, nil
}

func decode1251(r io.Reader) io.Reader {
	return transform.NewReader(r, charmap.Windows1251.NewDecoder())
}
