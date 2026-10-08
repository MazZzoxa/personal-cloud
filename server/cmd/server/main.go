package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	_ "modernc.org/sqlite"

	"personal-cloud/server/internal/api"
	"personal-cloud/server/internal/auth"
	"personal-cloud/server/internal/config"
	"personal-cloud/server/internal/database"
	"personal-cloud/server/internal/network"
	"personal-cloud/server/internal/storage"
)

const version = "0.5.0"

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
		ChatDir:    filepath.Join(root, "data", "chat-attachments"),
		Host:       "0.0.0.0",
		Port:       8080,
		// The host PC is trusted automatically. Set PC_TRUST_LOCALHOST=0 to
		// require pairing even on the host (e.g. behind a local reverse proxy).
		TrustLocalhost: os.Getenv("PC_TRUST_LOCALHOST") != "0",
	}

	for _, dir := range []string{cfg.DataDir, cfg.StorageDir, cfg.ChatDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("create directory %s: %v", dir, err)
		}
	}

	dbPath := filepath.Join(cfg.DataDir, "cloud.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)")
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if err := database.Initialize(db); err != nil {
		log.Fatalf("initialize database: %v", err)
	}

	authService, err := auth.NewService(db)
	if err != nil {
		log.Fatalf("initialize device identity: %v", err)
	}

	store := storage.New(cfg.StorageDir, db)
	handler := api.New(db, store, authService, cfg.WebDir, version, cfg.ChatDir, cfg.TrustLocalhost)

	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Println("Personal Cloud v" + version)
	log.Printf("Local:    http://localhost:%d", cfg.Port)
	lanIPs, tailscaleIPs := network.Discover(cfg.Port, "http")
	if len(lanIPs) > 0 {
		log.Printf("LAN:      http://%s:%d", lanIPs[0], cfg.Port)
	}
	if len(tailscaleIPs) > 0 {
		for _, ip := range tailscaleIPs {
			log.Printf("Tailscale: http://%s:%d", ip, cfg.Port)
		}
	} else {
		log.Println("Tailscale: not detected")
	}
	log.Printf("Storage: %s", cfg.StorageDir)
	log.Printf("Chat files: %s", cfg.ChatDir)
	log.Printf("Database: %s", dbPath)
	log.Printf("OS: %s/%s", runtime.GOOS, runtime.GOARCH)

	// A fresh single-use code lets the first remote device pair even before
	// any trusted device exists. It is shown only in this console.
	if code, expires, err := authService.CreatePairingCode(context.Background(), 0, 10*time.Minute); err == nil {
		log.Printf("Pairing code: %s (valid until %s)", auth.FormatCode(code), expires.Local().Format("15:04:05"))
	} else {
		log.Printf("could not create pairing code: %v", err)
	}

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
