// Package auth implements device identity for Personal Cloud (v0.4.0).
//
// Model:
//   - The host PC (direct loopback connections) is trusted automatically and is
//     represented by a single "host" device that can never be revoked.
//   - Every other device is paired once with a short-lived, single-use code. On
//     success the server issues a random device token. Only the SHA-256 hash of
//     the token is stored; the browser keeps the token in an HttpOnly cookie.
//   - A paired device stays trusted until it is revoked.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	CookieName = "pc_device"

	KindHost   = "host"
	KindPaired = "paired"

	codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // 32 symbols, no 0/O/1/I
	codeLength   = 8

	maxNameRunes = 60
	maxUserAgent = 300
	touchEvery   = time.Minute
)

var (
	ErrUnauthorized = errors.New("device is not authorized")
	ErrInvalidCode  = errors.New("invalid or expired pairing code")
	ErrNotFound     = errors.New("device not found")
	ErrHostDevice   = errors.New("the host device cannot be changed or revoked")
)

type Device struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	UserAgent  string `json:"userAgent"`
	CreatedAt  string `json:"createdAt"`
	LastSeenAt string `json:"lastSeenAt"`
	LastIP     string `json:"lastIp"`
}

type Service struct {
	db            *sql.DB
	hostID        int64
	IPLimiter     *Limiter
	GlobalLimiter *Limiter
}

func NewService(db *sql.DB) (*Service, error) {
	s := &Service{
		db:            db,
		IPLimiter:     NewLimiter(5, 10*time.Minute),
		GlobalLimiter: NewLimiter(30, 10*time.Minute),
	}
	if err := s.ensureHost(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func timestamp(t time.Time) string {
	// Fixed width, UTC: safe to compare lexicographically in SQL.
	return t.UTC().Format(time.RFC3339)
}

func hashSecret(prefix, value string) string {
	sum := sha256.Sum256([]byte(prefix + value))
	return hex.EncodeToString(sum[:])
}

func (s *Service) ensureHost(ctx context.Context) error {
	err := s.db.QueryRowContext(ctx, `SELECT id FROM devices WHERE kind = ? ORDER BY id LIMIT 1`, KindHost).Scan(&s.hostID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := timestamp(time.Now())
	res, err := s.db.ExecContext(ctx, `
INSERT INTO devices (name, kind, token_hash, user_agent, created_at, last_seen_at, last_ip)
VALUES (?, ?, NULL, '', ?, ?, '')`, "Компьютер-хост", KindHost, now, now)
	if err != nil {
		return err
	}
	s.hostID, err = res.LastInsertId()
	return err
}

const deviceColumns = `id, name, kind, user_agent, created_at, last_seen_at, last_ip`

func scanDevice(row interface{ Scan(...any) error }) (Device, error) {
	var d Device
	err := row.Scan(&d.ID, &d.Name, &d.Kind, &d.UserAgent, &d.CreatedAt, &d.LastSeenAt, &d.LastIP)
	return d, err
}

// Host returns the built-in device that represents the host PC.
func (s *Service) Host(ctx context.Context) (Device, error) {
	return s.Get(ctx, s.hostID)
}

func (s *Service) Get(ctx context.Context, id int64) (Device, error) {
	d, err := scanDevice(s.db.QueryRowContext(ctx,
		`SELECT `+deviceColumns+` FROM devices WHERE id = ? AND revoked_at IS NULL`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

// Authenticate resolves a device token from the cookie to a trusted device.
func (s *Service) Authenticate(ctx context.Context, token string) (Device, error) {
	if token == "" || len(token) > 128 {
		return Device{}, ErrUnauthorized
	}
	d, err := scanDevice(s.db.QueryRowContext(ctx,
		`SELECT `+deviceColumns+` FROM devices WHERE token_hash = ? AND revoked_at IS NULL`,
		hashSecret("pc-device:", token)))
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrUnauthorized
	}
	return d, err
}

// Touch records activity at most once per minute. It reports whether the row
// was updated, which the caller uses to refresh the long-lived cookie.
func (s *Service) Touch(ctx context.Context, d Device, ip string) bool {
	if last, err := time.Parse(time.RFC3339, d.LastSeenAt); err == nil && time.Since(last) < touchEvery {
		return false
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE devices SET last_seen_at = ?, last_ip = ? WHERE id = ?`, timestamp(time.Now()), ip, d.ID)
	return err == nil
}

func newCode() (string, error) {
	raw := make([]byte, codeLength)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	out := make([]byte, codeLength)
	for i, b := range raw {
		out[i] = codeAlphabet[b&31] // 256 is divisible by 32: no modulo bias
	}
	return string(out), nil
}

// FormatCode renders ABCDEFGH as ABCD-EFGH.
func FormatCode(code string) string {
	if len(code) != codeLength {
		return code
	}
	return code[:4] + "-" + code[4:]
}

func normalizeCode(value string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(value) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// CreatePairingCode creates a single-use code and invalidates every other
// unused code, so at most one code is ever active. creator may be 0 (server).
func (s *Service) CreatePairingCode(ctx context.Context, creator int64, ttl time.Duration) (string, time.Time, error) {
	code, err := newCode()
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now()
	expires := now.Add(ttl)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM pairing_codes WHERE used_at IS NULL OR expires_at < ?`, timestamp(now.Add(-24*time.Hour))); err != nil {
		return "", time.Time{}, err
	}
	var createdBy any
	if creator > 0 {
		createdBy = creator
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO pairing_codes (code_hash, created_by, created_at, expires_at)
VALUES (?, ?, ?, ?)`, hashSecret("pc-pair:", code), createdBy, timestamp(now), timestamp(expires)); err != nil {
		return "", time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return "", time.Time{}, err
	}
	return code, expires, nil
}

// Pair consumes a pairing code and registers a new trusted device. The
// returned token is shown only once; it must be handed to the browser cookie.
func (s *Service) Pair(ctx context.Context, code, name, userAgent, ip string) (Device, string, error) {
	normalized := normalizeCode(code)
	if len(normalized) != codeLength {
		return Device{}, "", ErrInvalidCode
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return Device{}, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)

	now := time.Now()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Device{}, "", err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `
UPDATE pairing_codes SET used_at = ?
WHERE code_hash = ? AND used_at IS NULL AND expires_at > ?`,
		timestamp(now), hashSecret("pc-pair:", normalized), timestamp(now))
	if err != nil {
		return Device{}, "", err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return Device{}, "", ErrInvalidCode
	}

	if len(userAgent) > maxUserAgent {
		userAgent = userAgent[:maxUserAgent]
	}
	insert, err := tx.ExecContext(ctx, `
INSERT INTO devices (name, kind, token_hash, user_agent, created_at, last_seen_at, last_ip)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
		cleanName(name), KindPaired, hashSecret("pc-device:", token), userAgent, timestamp(now), timestamp(now), ip)
	if err != nil {
		return Device{}, "", err
	}
	id, err := insert.LastInsertId()
	if err != nil {
		return Device{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return Device{}, "", err
	}

	device, err := s.Get(ctx, id)
	return device, token, err
}

func cleanName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	runes := []rune(strings.Join(strings.Fields(b.String()), " "))
	if len(runes) > maxNameRunes {
		runes = runes[:maxNameRunes]
	}
	if len(runes) == 0 {
		return "Новое устройство"
	}
	return string(runes)
}

// List returns every non-revoked device, host first, then most recent activity.
func (s *Service) List(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT `+deviceColumns+` FROM devices
WHERE revoked_at IS NULL
ORDER BY (kind = 'host') DESC, last_seen_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	devices := []Device{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}

func (s *Service) Rename(ctx context.Context, id int64, name string) (Device, error) {
	if id == s.hostID {
		return Device{}, ErrHostDevice
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE devices SET name = ? WHERE id = ? AND revoked_at IS NULL`, cleanName(name), id)
	if err != nil {
		return Device{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Device{}, ErrNotFound
	}
	return s.Get(ctx, id)
}

// Revoke removes trust from a paired device. Its token stops working at once.
func (s *Service) Revoke(ctx context.Context, id int64) error {
	if id == s.hostID {
		return ErrHostDevice
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE devices SET revoked_at = ?, token_hash = NULL WHERE id = ? AND revoked_at IS NULL`,
		timestamp(time.Now()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Limiter counts recent failures per key (brute-force protection for pairing).
type Limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	fails  map[string][]time.Time
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, fails: make(map[string][]time.Time)}
}

func (l *Limiter) prune(key string, now time.Time) []time.Time {
	recent := l.fails[key][:0]
	for _, t := range l.fails[key] {
		if now.Sub(t) < l.window {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		delete(l.fails, key)
		return nil
	}
	l.fails[key] = recent
	return recent
}

// Allow reports whether the key may still attempt an action.
func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key, time.Now())) < l.max
}

// Fail records a failed attempt.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.prune(key, now)
	l.fails[key] = append(l.fails[key], now)
}

// Reset forgets failures for a key after a success.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}
