package storage

import (
	"errors"
	"io"
	"strings"
)

// ErrNotText is returned when a text preview is requested for a binary file.
var ErrNotText = errors.New("file is not a text file")

// TextPreview is the beginning of a text file converted to UTF-8.
type TextPreview struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	Encoding  string `json:"encoding"`
	Lines     int    `json:"lines"`
	Truncated bool   `json:"truncated"`
}

// ReadText returns up to limit bytes of a text file (MaxTextPreviewBytes when
// limit <= 0) converted to UTF-8.
func (s *Store) ReadText(rel string, limit int64) (TextPreview, error) {
	file, info, clean, err := s.Open(rel)
	if err != nil {
		return TextPreview{}, err
	}
	defer file.Close()

	if limit <= 0 {
		limit = MaxTextPreviewBytes
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return TextPreview{}, err
	}
	truncated := int64(len(data)) > limit
	if truncated {
		data = data[:limit]
	}
	text, encoding, ok := decodeText(data, truncated)
	if !ok {
		return TextPreview{}, ErrNotText
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")

	lines := 0
	if text != "" {
		lines = strings.Count(text, "\n")
		if !strings.HasSuffix(text, "\n") {
			lines++
		}
	}
	return TextPreview{
		Name:      info.Name(),
		Path:      clean,
		Content:   text,
		Size:      info.Size(),
		Encoding:  encoding,
		Lines:     lines,
		Truncated: truncated,
	}, nil
}
