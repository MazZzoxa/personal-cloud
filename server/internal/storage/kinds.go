package storage

import (
	"bytes"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// File categories used by search filters and the UI.
const (
	CategoryImage    = "image"
	CategoryVideo    = "video"
	CategoryAudio    = "audio"
	CategoryDocument = "document"
	CategoryText     = "text"
	CategoryCode     = "code"
	CategoryArchive  = "archive"
	CategoryOther    = "other"
)

// Preview kinds tell the web UI how a file can be shown inline.
const (
	PreviewImage = "image"
	PreviewVideo = "video"
	PreviewAudio = "audio"
	PreviewPDF   = "pdf"
	PreviewText  = "text"
)

// MaxTextPreviewBytes is the amount of a text file returned by the text preview.
const MaxTextPreviewBytes = 512 * 1024

func extSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

var (
	imageExts   = extSet("png", "jpg", "jpeg", "gif", "webp", "avif", "bmp", "ico", "svg", "tif", "tiff", "heic", "heif")
	videoExts   = extSet("mp4", "m4v", "webm", "ogv", "mov", "mkv", "avi", "wmv", "flv", "mpg", "mpeg", "3gp")
	audioExts   = extSet("mp3", "wav", "ogg", "oga", "opus", "m4a", "aac", "flac", "wma", "aiff")
	archiveExts = extSet("zip", "rar", "7z", "tar", "gz", "tgz", "bz2", "xz", "zst")
	docExts     = extSet("pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "odt", "ods", "odp", "rtf", "epub", "pages", "numbers", "key")
	textExts    = extSet("txt", "md", "markdown", "log", "csv", "tsv", "json", "xml", "yaml", "yml", "toml", "ini", "cfg", "conf", "properties", "env", "gitignore", "gitattributes", "editorconfig", "rst", "tex", "srt", "vtt", "nfo")
	codeExts    = extSet(
		"go", "py", "js", "jsx", "ts", "tsx", "mjs", "cjs", "java", "kt", "kts", "swift", "dart",
		"c", "h", "cpp", "cc", "hpp", "cs", "rs", "rb", "php", "lua", "r", "scala",
		"sh", "bash", "zsh", "bat", "cmd", "ps1", "sql", "html", "htm", "css", "scss", "less", "vue", "svelte",
		"gradle", "mod", "sum", "lock", "dockerfile", "makefile",
	)

	// Files that have no extension but are plain text.
	textNames = extSet("readme", "license", "licence", "changelog", "authors", "contributors", "makefile", "dockerfile", "vagrantfile", "gemfile", "procfile", "version", ".gitignore", ".gitattributes", ".editorconfig", ".env")
)

// extensionOf returns the lowercase extension of a file name without the dot.
func extensionOf(name string) string {
	index := strings.LastIndex(name, ".")
	if index <= 0 || index == len(name)-1 {
		return ""
	}
	return strings.ToLower(name[index+1:])
}

// Categorize classifies a file by its name and (optional) MIME type.
func Categorize(name, mimeType string) string {
	ext := extensionOf(name)
	if ext != "" {
		switch {
		case has(imageExts, ext):
			return CategoryImage
		case has(videoExts, ext):
			return CategoryVideo
		case has(audioExts, ext):
			return CategoryAudio
		case has(archiveExts, ext):
			return CategoryArchive
		case has(docExts, ext):
			return CategoryDocument
		case has(codeExts, ext):
			return CategoryCode
		case has(textExts, ext):
			return CategoryText
		}
	} else if has(textNames, strings.ToLower(name)) {
		return CategoryText
	}

	mimeType = strings.ToLower(mimeType)
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return CategoryImage
	case strings.HasPrefix(mimeType, "video/"):
		return CategoryVideo
	case strings.HasPrefix(mimeType, "audio/"):
		return CategoryAudio
	case strings.HasPrefix(mimeType, "text/"):
		return CategoryText
	case mimeType == "application/pdf":
		return CategoryDocument
	case strings.Contains(mimeType, "zip") || strings.Contains(mimeType, "compressed") || strings.Contains(mimeType, "tar"):
		return CategoryArchive
	}
	return CategoryOther
}

func has(set map[string]struct{}, key string) bool {
	_, ok := set[key]
	return ok
}

// previewMimes is the allow-list of types that may be served inline. The
// content type is chosen here and never taken from the file itself.
var previewMimes = map[string]string{
	"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "gif": "image/gif",
	"webp": "image/webp", "avif": "image/avif", "bmp": "image/bmp", "ico": "image/x-icon", "svg": "image/svg+xml",
	"mp4": "video/mp4", "m4v": "video/mp4", "webm": "video/webm", "ogv": "video/ogg", "mov": "video/quicktime", "mkv": "video/x-matroska",
	"mp3": "audio/mpeg", "wav": "audio/wav", "ogg": "audio/ogg", "oga": "audio/ogg", "opus": "audio/ogg",
	"m4a": "audio/mp4", "aac": "audio/aac", "flac": "audio/flac",
	"pdf": "application/pdf",
}

// PreviewKind returns how a file can be previewed in the browser, or "" when
// preview is not supported.
func PreviewKind(name, mimeType string) string {
	ext := extensionOf(name)
	if ext != "" {
		if mt, ok := previewMimes[ext]; ok {
			switch {
			case strings.HasPrefix(mt, "image/"):
				return PreviewImage
			case strings.HasPrefix(mt, "video/"):
				return PreviewVideo
			case strings.HasPrefix(mt, "audio/"):
				return PreviewAudio
			default:
				return PreviewPDF
			}
		}
	}
	switch Categorize(name, mimeType) {
	case CategoryText, CategoryCode:
		return PreviewText
	}
	return ""
}

// PreviewContentType returns the inline content type for a binary preview
// (image, video, audio, pdf) or "" when the file cannot be served inline.
func PreviewContentType(name string) string {
	return previewMimes[extensionOf(name)]
}

// decodeText converts raw bytes into a UTF-8 string. It understands UTF-8
// (with or without BOM), UTF-16 with BOM and falls back to Windows-1251, which
// is common for older Russian text files. ok is false for binary data.
func decodeText(data []byte, truncated bool) (text, encoding string, ok bool) {
	switch {
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		return decodeUTF16(data[2:], false), "utf-16le", true
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		return decodeUTF16(data[2:], true), "utf-16be", true
	}

	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if bytes.IndexByte(data, 0) >= 0 {
		return "", "", false
	}
	if truncated {
		// Do not end in the middle of a multi-byte character.
		for i := 0; i < utf8.UTFMax && len(data) > 0; i++ {
			r, size := utf8.DecodeLastRune(data)
			if r == utf8.RuneError && size <= 1 {
				data = data[:len(data)-1]
				continue
			}
			break
		}
	}
	if utf8.Valid(data) {
		return string(data), "utf-8", true
	}
	if looksLikeText(data) {
		return decodeCP1251(data), "windows-1251", true
	}
	return "", "", false
}

func looksLikeText(data []byte) bool {
	for _, b := range data {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' && b != '\f' && b != 0x1B {
			return false
		}
	}
	return true
}

func decodeUTF16(data []byte, bigEndian bool) string {
	units := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		if bigEndian {
			units = append(units, uint16(data[i])<<8|uint16(data[i+1]))
		} else {
			units = append(units, uint16(data[i+1])<<8|uint16(data[i]))
		}
	}
	return string(utf16.Decode(units))
}

var cp1251High = [64]rune{
	0x0402, 0x0403, 0x201A, 0x0453, 0x201E, 0x2026, 0x2020, 0x2021, 0x20AC, 0x2030, 0x0409, 0x2039, 0x040A, 0x040C, 0x040B, 0x040F,
	0x0452, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0xFFFD, 0x2122, 0x0459, 0x203A, 0x045A, 0x045C, 0x045B, 0x045F,
	0x00A0, 0x040E, 0x045E, 0x0408, 0x00A4, 0x0490, 0x00A6, 0x00A7, 0x0401, 0x00A9, 0x0404, 0x00AB, 0x00AC, 0x00AD, 0x00AE, 0x0407,
	0x00B0, 0x00B1, 0x0406, 0x0456, 0x0491, 0x00B5, 0x00B6, 0x00B7, 0x0451, 0x2116, 0x0454, 0x00BB, 0x0458, 0x0405, 0x0455, 0x0457,
}

func decodeCP1251(data []byte) string {
	var builder strings.Builder
	builder.Grow(len(data) * 2)
	for _, b := range data {
		switch {
		case b < 0x80:
			builder.WriteByte(b)
		case b < 0xC0:
			builder.WriteRune(cp1251High[b-0x80])
		default:
			builder.WriteRune(0x0410 + rune(b-0xC0))
		}
	}
	return builder.String()
}
