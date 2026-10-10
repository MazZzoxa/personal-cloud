package storage

import (
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultSearchLimit = 300
	MaxSearchLimit     = 1000

	// Content search only looks into reasonably small text files and stops
	// after scanning a bounded amount of data so one query cannot run forever.
	maxContentFileBytes = 2 * 1024 * 1024
	maxContentScanBytes = 256 * 1024 * 1024
)

// SearchOptions describes a file search.
type SearchOptions struct {
	Query    string
	Path     string // limit the search to this folder ("" = whole storage)
	Category string // "", "folder" or one of the Category* values
	Modified int    // only items modified within this many days (0 = any time)
	Sort     string // relevance (default), name, size, modified, type
	Order    string // asc, desc or "" for the natural order of the sort key
	Content  bool   // also search inside text files
	Limit    int
}

// SearchHit is a search result with information about why it matched.
type SearchHit struct {
	Entry
	Match   string `json:"match"` // name, path or content
	Snippet string `json:"snippet,omitempty"`
	Line    int    `json:"line,omitempty"`
}

// SearchResult is the response of Store.Search.
type SearchResult struct {
	Items     []SearchHit `json:"items"`
	Terms     []string    `json:"terms"`     // normalized words, for highlighting
	Total     int         `json:"total"`     // all matches found
	Truncated bool        `json:"truncated"` // Total is larger than len(Items)
	Partial   bool        `json:"partial"`   // content scan stopped early
}

// ValidCategory reports whether value can be used as a search category filter.
func ValidCategory(value string) bool {
	switch value {
	case "", "folder", CategoryImage, CategoryVideo, CategoryAudio, CategoryDocument,
		CategoryText, CategoryCode, CategoryArchive, CategoryOther:
		return true
	}
	return false
}

type parsedQuery struct {
	terms    []string
	ext      string
	category string
}

type queryToken struct {
	text   string
	quoted bool
}

// splitQuery splits a query into words; "double quoted phrases" stay together.
func splitQuery(raw string) []queryToken {
	var tokens []queryToken
	var current []rune
	inQuotes := false
	wasQuoted := false
	flush := func() {
		if len(current) > 0 {
			tokens = append(tokens, queryToken{text: string(current), quoted: wasQuoted})
		}
		current = current[:0]
		wasQuoted = false
	}
	for _, r := range raw {
		switch {
		case r == '"':
			if inQuotes {
				flush()
			} else {
				flush()
				wasQuoted = true
			}
			inQuotes = !inQuotes
		case unicode.IsSpace(r) && !inQuotes:
			flush()
		default:
			current = append(current, r)
		}
	}
	flush()
	return tokens
}

func foldRune(r rune) rune {
	r = unicode.ToLower(r)
	if r == 'ё' {
		return 'е'
	}
	return r
}

// fold lower-cases text and treats "ё" as "е", keeping the rune count intact.
func fold(value string) string {
	return strings.Map(foldRune, value)
}

func parseQuery(raw string) parsedQuery {
	var query parsedQuery
	for _, token := range splitQuery(raw) {
		if !token.quoted {
			lower := strings.ToLower(token.text)
			if value, ok := strings.CutPrefix(lower, "ext:"); ok {
				value = strings.TrimPrefix(value, ".")
				if value != "" {
					query.ext = value
					continue
				}
			}
			if value, ok := strings.CutPrefix(lower, "type:"); ok && ValidCategory(value) && value != "" {
				query.category = value
				continue
			}
		}
		if folded := strings.TrimSpace(fold(token.text)); folded != "" {
			query.terms = append(query.terms, folded)
		}
	}
	return query
}

type candidate struct {
	rel      string
	info     os.FileInfo
	score    int
	match    string
	snippet  string
	line     int
	category string
}

// Search finds files and folders. Every word of the query has to match; words
// are matched against the name, path, extension and MIME type. With
// options.Content, text files are also searched by their content.
func (s *Store) Search(options SearchOptions) (SearchResult, error) {
	query := parseQuery(options.Query)
	category := options.Category
	if category == "" {
		category = query.category
	}
	if !ValidCategory(category) {
		return SearchResult{}, fmt.Errorf("unknown file type filter")
	}
	result := SearchResult{Items: []SearchHit{}, Terms: query.terms}
	if len(query.terms) == 0 && query.ext == "" && category == "" {
		return result, nil
	}

	limit := options.Limit
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}

	root := s.root
	if strings.TrimSpace(options.Path) != "" {
		abs, _, err := s.safePath(options.Path)
		if err != nil {
			return SearchResult{}, err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return SearchResult{}, err
		}
		if !info.IsDir() {
			return SearchResult{}, fmt.Errorf("search path is not a directory")
		}
		root = abs
	}

	var cutoff time.Time
	if options.Modified > 0 {
		cutoff = time.Now().Add(-time.Duration(options.Modified) * 24 * time.Hour)
	}

	whole := strings.Join(query.terms, " ")
	var candidates []candidate
	var scanned int64

	err := filepath.WalkDir(root, func(path string, dirEntry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Skip what cannot be read instead of failing the whole search.
			if dirEntry != nil && dirEntry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == root {
			return nil
		}
		name := dirEntry.Name()
		if strings.HasPrefix(name, ".upload-") {
			if dirEntry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := dirEntry.Info()
		if err != nil {
			return nil
		}
		relToStore, err := filepath.Rel(s.root, path)
		if err != nil {
			return nil
		}
		relToStore = filepath.ToSlash(relToStore)
		isDir := dirEntry.IsDir()

		ext := ""
		mimeGuess := ""
		itemCategory := "folder"
		if !isDir {
			ext = extensionOf(name)
			if ext != "" {
				mimeGuess = mime.TypeByExtension("." + ext)
			}
			itemCategory = Categorize(name, mimeGuess)
		}

		if category != "" && itemCategory != category {
			return nil
		}
		if query.ext != "" && ext != query.ext {
			return nil
		}
		if !cutoff.IsZero() && info.ModTime().Before(cutoff) {
			return nil
		}

		nameFolded := fold(name)
		pathFolded := fold(relToStore)
		extraFolded := fold(ext + " " + mimeGuess)

		score, matched := scoreMeta(query.terms, whole, nameFolded, pathFolded, extraFolded)
		if matched {
			match := "name"
			if len(query.terms) > 0 && !containsAll(nameFolded, query.terms) {
				match = "path"
			}
			score -= strings.Count(relToStore, "/")
			candidates = append(candidates, candidate{rel: relToStore, info: info, score: score, match: match, category: itemCategory})
			return nil
		}

		if options.Content && len(query.terms) > 0 && !isDir &&
			(itemCategory == CategoryText || itemCategory == CategoryCode) &&
			info.Size() > 0 && info.Size() <= maxContentFileBytes {
			if scanned+info.Size() > maxContentScanBytes {
				result.Partial = true
				return nil
			}
			scanned += info.Size()
			if snippet, line, ok := searchContent(path, query.terms); ok {
				candidates = append(candidates, candidate{
					rel: relToStore, info: info, score: 20 - strings.Count(relToStore, "/"),
					match: "content", snippet: snippet, line: line, category: itemCategory,
				})
			}
		}
		return nil
	})
	if err != nil {
		return SearchResult{}, err
	}

	sortCandidates(candidates, options.Sort, options.Order)

	result.Total = len(candidates)
	if len(candidates) > limit {
		candidates = candidates[:limit]
		result.Truncated = true
	}
	for _, item := range candidates {
		entry, err := s.entryFromInfo(item.rel, item.info)
		if err != nil {
			return SearchResult{}, err
		}
		result.Items = append(result.Items, SearchHit{Entry: entry, Match: item.match, Snippet: item.snippet, Line: item.line})
	}
	return result, nil
}

func containsAll(haystack string, terms []string) bool {
	for _, term := range terms {
		if !strings.Contains(haystack, term) {
			return false
		}
	}
	return true
}

// scoreMeta checks that every term occurs in the name, path, extension or MIME
// type and returns a relevance score. Name matches rank above path matches.
func scoreMeta(terms []string, whole, name, path, extra string) (int, bool) {
	if len(terms) == 0 {
		// Only filters (ext:/type:) were given: everything that passed matches.
		return 1, true
	}
	score := 0
	for _, term := range terms {
		switch {
		case strings.Contains(name, term):
			score += 60
			if atWordStart(name, term) {
				score += 40
			}
		case strings.Contains(path, term):
			score += 30
		case strings.Contains(extra, term):
			score += 10
		default:
			return 0, false
		}
	}
	nameNoExt := name
	if index := strings.LastIndex(name, "."); index > 0 {
		nameNoExt = name[:index]
	}
	switch {
	case name == whole:
		score += 1000
	case nameNoExt == whole:
		score += 900
	case strings.HasPrefix(name, whole):
		score += 300
	}
	return score, true
}

// atWordStart reports whether term occurs in s at the start of a word.
func atWordStart(s, term string) bool {
	offset := 0
	for offset <= len(s) {
		index := strings.Index(s[offset:], term)
		if index < 0 {
			return false
		}
		index += offset
		if index == 0 {
			return true
		}
		previous, _ := utf8.DecodeLastRuneInString(s[:index])
		if !unicode.IsLetter(previous) && !unicode.IsDigit(previous) {
			return true
		}
		offset = index + 1
	}
	return false
}

// searchContent reads a text file and returns a snippet around the first
// occurrence of the first term when every term is present in the file.
func searchContent(path string, terms []string) (snippet string, line int, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, false
	}
	text, _, isText := decodeText(data, false)
	if !isText {
		return "", 0, false
	}
	folded := fold(text)
	if !containsAll(folded, terms) {
		return "", 0, false
	}
	index := strings.Index(folded, terms[0])
	if index < 0 {
		return "", 0, false
	}
	runeIndex := utf8.RuneCountInString(folded[:index])
	line = 1 + strings.Count(folded[:index], "\n")

	runes := []rune(text)
	start := runeIndex - 60
	if start < 0 {
		start = 0
	}
	end := runeIndex + utf8.RuneCountInString(terms[0]) + 100
	if end > len(runes) {
		end = len(runes)
	}
	if start > end {
		start = end
	}
	snippet = strings.Join(strings.Fields(string(runes[start:end])), " ")
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < len(runes) {
		snippet += "…"
	}
	return snippet, line, true
}

func sortCandidates(items []candidate, key, order string) {
	defaultDesc := false
	var compare func(a, b candidate) int
	switch key {
	case "name":
		compare = func(a, b candidate) int {
			return strings.Compare(strings.ToLower(a.info.Name()), strings.ToLower(b.info.Name()))
		}
	case "size":
		defaultDesc = true
		compare = func(a, b candidate) int { return compareInt64(a.info.Size(), b.info.Size()) }
	case "modified":
		defaultDesc = true
		compare = func(a, b candidate) int {
			return compareInt64(a.info.ModTime().UnixNano(), b.info.ModTime().UnixNano())
		}
	case "type":
		compare = func(a, b candidate) int {
			if c := strings.Compare(a.category, b.category); c != 0 {
				return c
			}
			return strings.Compare(extensionOf(a.info.Name()), extensionOf(b.info.Name()))
		}
	default: // relevance
		defaultDesc = true
		compare = func(a, b candidate) int { return compareInt64(int64(a.score), int64(b.score)) }
	}
	desc := defaultDesc
	switch order {
	case "asc":
		desc = false
	case "desc":
		desc = true
	}
	sort.SliceStable(items, func(i, j int) bool {
		c := compare(items[i], items[j])
		if c == 0 {
			return strings.ToLower(items[i].rel) < strings.ToLower(items[j].rel)
		}
		if desc {
			return c > 0
		}
		return c < 0
	})
}

func compareInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
