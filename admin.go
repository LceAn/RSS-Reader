package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	adminPasswordEnvironment     = "RSS_READER_ADMIN_PASSWORD"
	adminPasswordFileEnvironment = "RSS_READER_ADMIN_PASSWORD_FILE"
	adminSessionCookie           = "rss_reader_admin"
	adminSessionLifetime         = 12 * time.Hour
	loginFailureLimit            = 5
	loginBlockDuration           = 5 * time.Minute
	minimumAdminPasswordLength   = 12
)

type adminService struct {
	enabled        bool
	passwordDigest [sha256.Size]byte
	now            func() time.Time

	sessionMu sync.Mutex
	sessions  map[[sha256.Size]byte]adminSession

	loginMu       sync.Mutex
	loginAttempts map[string]loginAttempt

	configMu sync.Mutex
}

type adminSession struct {
	CSRFToken string
	ExpiresAt time.Time
}

type loginAttempt struct {
	Failures     int
	FirstFailure time.Time
	BlockedUntil time.Time
}

type sessionResponse struct {
	Enabled       bool   `json:"enabled"`
	Authenticated bool   `json:"authenticated"`
	CSRFToken     string `json:"csrfToken,omitempty"`
}

func newAdminServiceFromEnvironment() (*adminService, error) {
	password := os.Getenv(adminPasswordEnvironment)
	if passwordFile := strings.TrimSpace(os.Getenv(adminPasswordFileEnvironment)); passwordFile != "" {
		data, err := os.ReadFile(passwordFile)
		if err != nil {
			return nil, fmt.Errorf("read admin password file: %w", err)
		}
		password = strings.TrimRight(string(data), "\r\n")
	}
	return newAdminService(password)
}

func newAdminService(password string) (*adminService, error) {
	service := &adminService{
		now:           time.Now,
		sessions:      make(map[[sha256.Size]byte]adminSession),
		loginAttempts: make(map[string]loginAttempt),
	}
	if password == "" {
		return service, nil
	}
	if len(password) < minimumAdminPasswordLength {
		return nil, fmt.Errorf("%s must contain at least %d characters", adminPasswordEnvironment, minimumAdminPasswordLength)
	}
	service.enabled = true
	service.passwordDigest = sha256.Sum256([]byte(password))
	return service, nil
}

func (service *adminService) register(mux *http.ServeMux) {
	mux.HandleFunc("/api/auth/session", service.sessionHandler)
	mux.HandleFunc("/api/auth/login", service.loginHandler)
	mux.HandleFunc("/api/auth/logout", service.logoutHandler)
	mux.HandleFunc("/api/admin/config", service.adminConfigHandler)
	mux.HandleFunc("/api/admin/opml", service.adminOPMLHandler)
}

func (service *adminService) sessionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	response := sessionResponse{Enabled: service.enabled}
	if session, ok := service.authenticate(r); ok {
		response.Authenticated = true
		response.CSRFToken = session.CSRFToken
	}
	writeJSON(w, http.StatusOK, response)
}

func (service *adminService) loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	if !service.enabled {
		writeAPIError(w, http.StatusServiceUnavailable, "管理员登录未配置")
		return
	}
	if !requestHasSameOrigin(r) {
		writeAPIError(w, http.StatusForbidden, "请求来源无效")
		return
	}

	client := remoteHost(r.RemoteAddr)
	if allowed, retryAfter := service.loginAllowed(client); !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", max(1, int(retryAfter.Seconds()))))
		writeAPIError(w, http.StatusTooManyRequests, "尝试次数过多，请稍后再试")
		return
	}

	var payload struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, 4096, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "请求格式无效")
		return
	}
	candidate := sha256.Sum256([]byte(payload.Password))
	if subtle.ConstantTimeCompare(candidate[:], service.passwordDigest[:]) != 1 {
		service.recordLoginFailure(client)
		writeAPIError(w, http.StatusUnauthorized, "密码错误")
		return
	}
	service.clearLoginFailures(client)

	sessionToken, err := randomToken()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "无法创建登录会话")
		return
	}
	csrfToken, err := randomToken()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "无法创建登录会话")
		return
	}
	now := service.now()
	service.sessionMu.Lock()
	service.removeCookieSessionLocked(r)
	service.sessions[sessionKey(sessionToken)] = adminSession{
		CSRFToken: csrfToken,
		ExpiresAt: now.Add(adminSessionLifetime),
	}
	service.removeExpiredSessionsLocked(now)
	service.sessionMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    sessionToken,
		Path:     "/",
		MaxAge:   int(adminSessionLifetime.Seconds()),
		HttpOnly: true,
		Secure:   requestUsesHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, sessionResponse{
		Enabled:       true,
		Authenticated: true,
		CSRFToken:     csrfToken,
	})
}

func (service *adminService) logoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	session, ok := service.authenticate(r)
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "登录信息已失效")
		return
	}
	if !requestHasSameOrigin(r) || !csrfMatches(r.Header.Get("X-CSRF-Token"), session.CSRFToken) {
		writeAPIError(w, http.StatusForbidden, "安全校验失败")
		return
	}
	service.sessionMu.Lock()
	service.removeCookieSessionLocked(r)
	service.sessionMu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestUsesHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, sessionResponse{Enabled: service.enabled})
}

func (service *adminService) authenticate(r *http.Request) (adminSession, bool) {
	if !service.enabled {
		return adminSession{}, false
	}
	cookie, err := r.Cookie(adminSessionCookie)
	if err != nil || cookie.Value == "" {
		return adminSession{}, false
	}
	key := sessionKey(cookie.Value)
	now := service.now()
	service.sessionMu.Lock()
	defer service.sessionMu.Unlock()
	session, exists := service.sessions[key]
	if !exists || !session.ExpiresAt.After(now) {
		delete(service.sessions, key)
		return adminSession{}, false
	}
	return session, true
}

func (service *adminService) removeCookieSessionLocked(r *http.Request) {
	cookie, err := r.Cookie(adminSessionCookie)
	if err == nil && cookie.Value != "" {
		delete(service.sessions, sessionKey(cookie.Value))
	}
}

func (service *adminService) removeExpiredSessionsLocked(now time.Time) {
	for key, session := range service.sessions {
		if !session.ExpiresAt.After(now) {
			delete(service.sessions, key)
		}
	}
}

func (service *adminService) loginAllowed(client string) (bool, time.Duration) {
	now := service.now()
	service.loginMu.Lock()
	defer service.loginMu.Unlock()
	attempt, exists := service.loginAttempts[client]
	if !exists {
		return true, 0
	}
	if attempt.BlockedUntil.After(now) {
		return false, attempt.BlockedUntil.Sub(now)
	}
	if now.Sub(attempt.FirstFailure) >= loginBlockDuration {
		delete(service.loginAttempts, client)
	}
	return true, 0
}

func (service *adminService) recordLoginFailure(client string) {
	now := service.now()
	service.loginMu.Lock()
	defer service.loginMu.Unlock()
	attempt := service.loginAttempts[client]
	if attempt.FirstFailure.IsZero() || now.Sub(attempt.FirstFailure) >= loginBlockDuration {
		attempt = loginAttempt{FirstFailure: now}
	}
	attempt.Failures++
	if attempt.Failures >= loginFailureLimit {
		attempt.BlockedUntil = now.Add(loginBlockDuration)
	}
	service.loginAttempts[client] = attempt
}

func (service *adminService) clearLoginFailures(client string) {
	service.loginMu.Lock()
	delete(service.loginAttempts, client)
	service.loginMu.Unlock()
}

func decodeJSON(w http.ResponseWriter, r *http.Request, maxBytes int64, target interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request body must contain one JSON value")
	}
	return nil
}

func requestHasSameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

func requestUsesHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func csrfMatches(provided, expected string) bool {
	providedDigest := sha256.Sum256([]byte(provided))
	expectedDigest := sha256.Sum256([]byte(expected))
	return provided != "" && subtle.ConstantTimeCompare(providedDigest[:], expectedDigest[:]) == 1
}

func randomToken() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func sessionKey(token string) [sha256.Size]byte {
	return sha256.Sum256([]byte(token))
}

func remoteHost(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		return host
	}
	return address
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	writeAPIError(w, http.StatusMethodNotAllowed, "请求方法不受支持")
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
