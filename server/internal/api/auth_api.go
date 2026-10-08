package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"personal-cloud/server/internal/auth"
	"personal-cloud/server/internal/network"
)

const (
	pairingTTL     = 5 * time.Minute
	cookieLifetime = 365 * 24 * 60 * 60
)

var errUnauthorized = errors.New("Требуется подключённое устройство")

type deviceContextKey struct{}

func deviceFromContext(ctx context.Context) (auth.Device, bool) {
	device, ok := ctx.Value(deviceContextKey{}).(auth.Device)
	return device, ok
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// isDirectLoopback is true only for a connection made straight from the host
// PC. Requests that carry proxy headers (Vite dev proxy, Tailscale Serve or any
// other reverse proxy) are never treated as the host, even though they arrive
// from 127.0.0.1.
func isDirectLoopback(r *http.Request) bool {
	for _, header := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Real-Ip", "Forwarded"} {
		if r.Header.Get(header) != "" {
			return false
		}
	}
	ip := net.ParseIP(clientIP(r))
	return ip != nil && ip.IsLoopback()
}

func isSecureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func setDeviceCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   cookieLifetime,
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func clearDeviceCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// resolveDevice identifies the caller: the host PC, or a paired device that
// presents a valid cookie.
func (s *Server) resolveDevice(w http.ResponseWriter, r *http.Request) (auth.Device, bool) {
	ctx := r.Context()

	if s.trustLocalhost && isDirectLoopback(r) {
		if device, err := s.auth.Host(ctx); err == nil {
			s.auth.Touch(ctx, device, clientIP(r))
			return device, true
		}
	}

	cookie, err := r.Cookie(auth.CookieName)
	if err != nil || cookie.Value == "" {
		return auth.Device{}, false
	}
	device, err := s.auth.Authenticate(ctx, cookie.Value)
	if err != nil {
		if errors.Is(err, auth.ErrUnauthorized) {
			clearDeviceCookie(w, r) // revoked or unknown token
		}
		return auth.Device{}, false
	}
	if s.auth.Touch(ctx, device, clientIP(r)) {
		setDeviceCookie(w, r, cookie.Value) // keep the long-lived cookie fresh
	}
	return device, true
}

func isPublicAPI(r *http.Request) bool {
	switch r.Method + " " + r.URL.Path {
	case "GET /api/health", "GET /api/auth/me", "GET /api/network", "POST /api/auth/pair":
		return true
	}
	return false
}

// withAuth protects every /api/ route except the public ones above. Static
// frontend files stay public so the pairing screen can load.
func (s *Server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		device, ok := s.resolveDevice(w, r)
		if ok {
			r = r.WithContext(context.WithValue(r.Context(), deviceContextKey{}, device))
		}
		if ok || isPublicAPI(r) {
			next.ServeHTTP(w, r)
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": errUnauthorized.Error(),
			"code":  "unauthorized",
		})
	})
}

// withOriginCheck is CSRF protection for state-changing requests: browsers
// attach an Origin header, which must match the host that served the app.
func (s *Server) withOriginCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(parsed.Host, r.Host) {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "Запрос с чужого источника отклонён"})
				return
			}
		} else if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "Запрос с чужого источника отклонён"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

type deviceView struct {
	auth.Device
	Current bool `json:"current"`
	Online  bool `json:"online"`
}

func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}

// GET /api/auth/me — public; tells the frontend whether to show the pairing screen.
func (s *Server) authMe(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	device, ok := deviceFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false, "version": s.version})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"version":       s.version,
		"isHost":        device.Kind == auth.KindHost,
		"device":        deviceView{Device: device, Current: true},
	})
}

// POST /api/auth/pair — public; exchanges a pairing code for a device cookie.
func (s *Server) authPair(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	ip := clientIP(r)
	if !s.auth.IPLimiter.Allow(ip) || !s.auth.GlobalLimiter.Allow("*") {
		errorJSON(w, http.StatusTooManyRequests, fmt.Errorf("Слишком много неудачных попыток. Подождите несколько минут."))
		return
	}

	var payload struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}

	device, token, err := s.auth.Pair(r.Context(), payload.Code, payload.Name, r.UserAgent(), ip)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCode) {
			s.auth.IPLimiter.Fail(ip)
			s.auth.GlobalLimiter.Fail("*")
			errorJSON(w, http.StatusForbidden, fmt.Errorf("Неверный или просроченный код"))
			return
		}
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}

	s.auth.IPLimiter.Reset(ip)
	setDeviceCookie(w, r, token)
	writeJSON(w, http.StatusCreated, deviceView{Device: device, Current: true})
}

// POST /api/auth/logout — the current paired device forgets its own trust.
func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	device, _ := deviceFromContext(r.Context())
	if device.Kind == auth.KindPaired {
		if err := s.auth.Revoke(r.Context(), device.ID); err != nil && !errors.Is(err, auth.ErrNotFound) {
			errorJSON(w, http.StatusInternalServerError, err)
			return
		}
		s.chatHub.DisconnectDevice(device.ID)
		clearDeviceCookie(w, r)
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/devices
func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	devices, err := s.auth.List(r.Context())
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	current, _ := deviceFromContext(r.Context())
	online := s.chatHub.OnlineDevices()

	items := make([]deviceView, 0, len(devices))
	for _, device := range devices {
		items = append(items, deviceView{Device: device, Current: device.ID == current.ID, Online: online[device.ID]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// POST /api/devices/pairing — create a single-use code for a new device.
func (s *Server) createPairingCode(w http.ResponseWriter, r *http.Request) {
	noStore(w)
	current, _ := deviceFromContext(r.Context())
	code, expires, err := s.auth.CreatePairingCode(r.Context(), current.ID, pairingTTL)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, err)
		return
	}
	formatted := auth.FormatCode(code)
	routes := network.Routes(requestPort(r), requestScheme(r))
	writeJSON(w, http.StatusCreated, map[string]any{
		"code":       formatted,
		"expiresAt":  expires.UTC().Format(time.RFC3339),
		"ttlSeconds": int(pairingTTL.Seconds()),
		"urls":       pairingURLs(r, formatted),
		"routes":     routes,
	})
}

// pairingURLs lists addresses other devices on the network can open; the code
// travels in the URL fragment, so it is never sent to the server in the request.
func pairingURLs(r *http.Request, code string) []string {
	routes := network.Routes(requestPort(r), requestScheme(r))
	urls := make([]string, 0, len(routes))
	for _, route := range routes {
		urls = append(urls, route.URL+"/#pair="+url.QueryEscape(code))
	}
	return urls
}

func deviceIDFromPath(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid device id")
	}
	return id, nil
}

func deviceErrorStatus(err error) int {
	switch {
	case errors.Is(err, auth.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, auth.ErrHostDevice):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

// PATCH /api/devices/{id}
func (s *Server) renameDevice(w http.ResponseWriter, r *http.Request) {
	id, err := deviceIDFromPath(r)
	if err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	var payload struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	device, err := s.auth.Rename(r.Context(), id, payload.Name)
	if err != nil {
		errorJSON(w, deviceErrorStatus(err), err)
		return
	}
	current, _ := deviceFromContext(r.Context())
	writeJSON(w, http.StatusOK, deviceView{Device: device, Current: device.ID == current.ID})
}

// DELETE /api/devices/{id} — revoke access and drop live connections.
func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	id, err := deviceIDFromPath(r)
	if err != nil {
		errorJSON(w, http.StatusBadRequest, err)
		return
	}
	if err := s.auth.Revoke(r.Context(), id); err != nil {
		errorJSON(w, deviceErrorStatus(err), err)
		return
	}
	s.chatHub.DisconnectDevice(id)

	if current, ok := deviceFromContext(r.Context()); ok && current.ID == id {
		clearDeviceCookie(w, r)
	}
	w.WriteHeader(http.StatusNoContent)
}
