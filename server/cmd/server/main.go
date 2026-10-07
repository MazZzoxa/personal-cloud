package main

import (
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	_ "modernc.org/sqlite"

	"personal-cloud/server/internal/api"
	"personal-cloud/server/internal/config"
	"personal-cloud/server/internal/database"
	"personal-cloud/server/internal/storage"
)

const version = "0.2.0"

func main() {
	root, err := projectRoot()
	if err != nil {
		log.Fatal(err)
	}

	cfg := config.Config{
		RootDir:    root,
		DataDir:    filepath.Join(root, "data"),
		StorageDir: filepath.Join(root, "storage"),
		WebDir:     filepath.Join(root, "web", "dist"),
		Host:       "0.0.0.0",
		Port:       8080,
	}

	for _, dir := range []string{cfg.DataDir, cfg.StorageDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("create directory %s: %v", dir, err)
		}
	}

	dbPath := filepath.Join(cfg.DataDir, "cloud.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if err := database.Initialize(db); err != nil {
		log.Fatalf("initialize database: %v", err)
	}

	store := storage.New(cfg.StorageDir, db)
	handler := api.New(db, store, cfg.WebDir, version)

	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Println("Personal Cloud v" + version)
	log.Printf("Local:   http://localhost:%d", cfg.Port)
	if ip := localIPv4(); ip != "" {
		log.Printf("LAN:     http://%s:%d", ip, cfg.Port)
	}
	log.Printf("Storage: %s", cfg.StorageDir)
	log.Printf("Database: %s", dbPath)
	log.Printf("OS: %s/%s", runtime.GOOS, runtime.GOARCH)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func projectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Support both `go run ./cmd/server` from server/ and
	// `go run ./server/cmd/server` from the repository root.
	if _, err := os.Stat(filepath.Join(cwd, "web")); err == nil {
		return cwd, nil
	}
	parent := filepath.Dir(cwd)
	if _, err := os.Stat(filepath.Join(parent, "web")); err == nil {
		return parent, nil
	}
	return cwd, nil
}

func localIPv4() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			var ip net.IP
			switch value := address.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			ip = ip.To4()
			if ip != nil {
				return ip.String()
			}
		}
	}
	return ""
}
