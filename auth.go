package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// AuthSecret is the HMAC signing key for tokens
var AuthSecret []byte

// InitAuthSecret initializes the authentication secret
func InitAuthSecret() {
	AuthSecret = make([]byte, 32)
	if _, err := rand.Read(AuthSecret); err != nil {
		slog.Error("failed to generate auth secret", "error", err)
		os.Exit(1)
	}
}

// GenerateToken creates a 24-hour HMAC token for main auth
func GenerateToken(password string) string {
	expiry := time.Now().Add(24 * time.Hour).Unix()
	payload := fmt.Sprintf("%d:%s", expiry, password)
	mac := hmac.New(sha256.New, AuthSecret)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%d:%s", expiry, sig)
}

// ValidateToken validates a main auth token
func ValidateToken(token, password string) bool {
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return false
	}
	expiry, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}
	if time.Now().Unix() > expiry {
		return false
	}
	payload := fmt.Sprintf("%d:%s", expiry, password)
	mac := hmac.New(sha256.New, AuthSecret)
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(parts[1]), []byte(expected))
}

// GenerateShellToken creates a 1-hour token for shell access
func GenerateShellToken(shellPassword string) string {
	expiry := time.Now().Add(1 * time.Hour).Unix()
	payload := fmt.Sprintf("shell:%d:%s", expiry, shellPassword)
	mac := hmac.New(sha256.New, AuthSecret)
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%d:%s", expiry, sig)
}

// ValidateShellToken validates a shell access token
func ValidateShellToken(token, shellPassword string) bool {
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return false
	}
	expiry, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}
	if time.Now().Unix() > expiry {
		return false
	}
	payload := fmt.Sprintf("shell:%d:%s", expiry, shellPassword)
	mac := hmac.New(sha256.New, AuthSecret)
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(parts[1]), []byte(expected))
}

// IsAuthenticated checks if the request is authenticated
func IsAuthenticated(r *http.Request, password string) bool {
	if password == "" {
		return true
	}
	if t := r.URL.Query().Get("token"); t != "" {
		return ValidateToken(t, password)
	}
	if c, err := r.Cookie("sysmon_token"); err == nil {
		return ValidateToken(c.Value, password)
	}
	return false
}

// AuthRequired is middleware that redirects to login if not authenticated
func AuthRequired(password string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !IsAuthenticated(r, password) {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

// AllowedOrigins are set via ALLOWED_ORIGINS env var
var AllowedOrigins []string

// InitAllowedOrigins initializes allowed origins from environment
func InitAllowedOrigins() {
	if v := os.Getenv("ALLOWED_ORIGINS"); v != "" {
		for _, o := range strings.Split(v, ",") {
			o = strings.TrimSpace(o)
			if o != "" {
				AllowedOrigins = append(AllowedOrigins, o)
			}
		}
	}
}

// CheckOrigin validates WebSocket origin
func CheckOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, o := range AllowedOrigins {
		if origin == o {
			return true
		}
	}
	host := r.Host
	if host == "" {
		return false
	}
	if strings.HasPrefix(origin, "http://") || strings.HasPrefix(origin, "https://") {
		originHost := origin[strings.Index(origin, "//")+2:]
		return originHost == host
	}
	return false
}
