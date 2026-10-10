package storage

import "testing"

func TestCategorizeAndPreviewKind(t *testing.T) {
	cases := []struct {
		name, category, preview string
	}{
		{"photo.PNG", CategoryImage, PreviewImage},
		{"movie.mkv", CategoryVideo, PreviewVideo},
		{"song.flac", CategoryAudio, PreviewAudio},
		{"paper.pdf", CategoryDocument, PreviewPDF},
		{"letter.docx", CategoryDocument, ""},
		{"backup.zip", CategoryArchive, ""},
		{"main.go", CategoryCode, PreviewText},
		{"notes.md", CategoryText, PreviewText},
		{"README", CategoryText, PreviewText},
		{"program.exe", CategoryOther, ""},
	}
	for _, c := range cases {
		if got := Categorize(c.name, ""); got != c.category {
			t.Errorf("Categorize(%q) = %q, want %q", c.name, got, c.category)
		}
		if got := PreviewKind(c.name, ""); got != c.preview {
			t.Errorf("PreviewKind(%q) = %q, want %q", c.name, got, c.preview)
		}
	}
}

func TestDecodeText(t *testing.T) {
	if text, enc, ok := decodeText([]byte("привет"), false); !ok || text != "привет" || enc != "utf-8" {
		t.Errorf("utf-8: %q %q %v", text, enc, ok)
	}
	cp1251 := []byte{0xCF, 0xF0, 0xE8, 0xE2, 0xE5, 0xF2}
	if text, enc, ok := decodeText(cp1251, false); !ok || text != "Привет" || enc != "windows-1251" {
		t.Errorf("windows-1251: %q %q %v", text, enc, ok)
	}
	utf16le := []byte{0xFF, 0xFE, 'H', 0, 'i', 0}
	if text, enc, ok := decodeText(utf16le, false); !ok || text != "Hi" || enc != "utf-16le" {
		t.Errorf("utf-16le: %q %q %v", text, enc, ok)
	}
	if _, _, ok := decodeText([]byte{0x89, 'P', 'N', 'G', 0x00, 0x01}, false); ok {
		t.Error("binary data must not be treated as text")
	}
	if text, _, ok := decodeText([]byte("привет")[:5], true); !ok || text != "пр" {
		t.Errorf("truncated utf-8: %q %v", text, ok)
	}
}
