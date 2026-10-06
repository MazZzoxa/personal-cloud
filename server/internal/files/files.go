package files

// Package files contains file-domain types. The first release keeps most file
// operations in the storage package; this package is reserved for later
// separation as the backend grows.

type File struct {
	ID   int64
	Path string
	Name string
}
