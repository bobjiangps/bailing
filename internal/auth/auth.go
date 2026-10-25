// Package auth implements users, sessions, login rate limiting and the
// HTTP middleware that attaches the current user to the request context.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"bailing/internal/store"
)

// CookieName is the session cookie.
const CookieName = "bl_session"

var (
	ErrBadCredentials = errors.New("bad credentials")
	ErrLocked         = errors.New("too many failures, locked")
	ErrNotFound       = errors.New("user not found")
)

// User is the authenticated principal.
type User struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
}

// Service bundles user/session operations.
type Service struct {
	db         *store.DB
	sessionTTL time.Duration
	noAuth     bool // auth.mode == none (desktop / local only)
	limiter    *limiter
}

// NewService creates the auth service.
func NewService(db *store.DB, sessionDays int, noAuth bool,
	maxFailures int, window, lock time.Duration) *Service {
	if sessionDays <= 0 {
		sessionDays = 30
	}
	return &Service{
		db:         db,
		sessionTTL: time.Duration(sessionDays) * 24 * time.Hour,
		noAuth:     noAuth,
		limiter:    newLimiter(maxFailures, window, lock),
	}
}

// NoAuth reports whether login is disabled (local desktop mode).
func (s *Service) NoAuth() bool { return s.noAuth }

// SessionTTL returns the session lifetime.
func (s *Service) SessionTTL() time.Duration { return s.sessionTTL }

// ---- users ----------------------------------------------------------------

// AddUser creates a user with a hashed password.
func (s *Service) AddUser(name, display, password string, admin bool) error {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" || len(password) < 6 {
		return errors.New("name required and password must be at least 6 characters")
	}
	h, err := HashPassword(password)
	if err != nil {
		return err
	}
	if display == "" {
		display = name
	}
	_, err = s.db.Exec(`INSERT INTO users(name, display_name, pw_hash, is_admin, created_at)
		VALUES(?,?,?,?,?)`, name, display, h, store.Bool(admin), time.Now().Unix())
	return err
}

// SetPassword changes a user's password and revokes their sessions.
func (s *Service) SetPassword(name, password string) error {
	if len(password) < 6 {
		return errors.New("password must be at least 6 characters")
	}
	h, err := HashPassword(password)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE users SET pw_hash=? WHERE name=?`, h, strings.ToLower(name))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = s.db.Exec(`DELETE FROM sessions WHERE user_id=(SELECT id FROM users WHERE name=?)`,
		strings.ToLower(name))
	return err
}

// DeleteUser removes a user (sessions cascade).
func (s *Service) DeleteUser(name string) error {
	res, err := s.db.Exec(`DELETE FROM users WHERE name=?`, strings.ToLower(name))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListUsers returns all users.
func (s *Service) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT id, name, display_name, is_admin FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.DisplayName, &u.IsAdmin); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// LocalUser returns (creating if needed) the implicit user for auth.mode=none.
func (s *Service) LocalUser() (*User, error) {
	u := &User{}
	err := s.db.QueryRow(`SELECT id, name, display_name, is_admin FROM users ORDER BY id LIMIT 1`).
		Scan(&u.ID, &u.Name, &u.DisplayName, &u.IsAdmin)
	if errors.Is(err, sql.ErrNoRows) {
		if err := s.AddUser("local", "Me", randomToken()[:16], true); err != nil {
			return nil, err
		}
		return s.LocalUser()
	}
	return u, err
}

// ---- login / sessions -----------------------------------------------------

// Login verifies credentials (with rate limiting) and creates a session.
// It returns the raw token; only its SHA-256 is stored.
func (s *Service) Login(ip, name, password, userAgent, kind string) (string, *User, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	if s.limiter.locked(ip) || s.limiter.locked("u:"+name) {
		return "", nil, ErrLocked
	}
	u := &User{}
	var hash string
	err := s.db.QueryRow(`SELECT id, name, display_name, is_admin, pw_hash FROM users WHERE name=?`, name).
		Scan(&u.ID, &u.Name, &u.DisplayName, &u.IsAdmin, &hash)
	if err != nil {
		// Run a dummy verification so timing does not reveal whether the user exists.
		VerifyPassword("$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2FsdA$"+
			"aGFzaGhhc2hoYXNoaGFzaGhhc2hoYXNoaGFzaGhhc2g", password)
		s.limiter.fail(ip)
		s.limiter.fail("u:" + name)
		return "", nil, ErrBadCredentials
	}
	ok, err := VerifyPassword(hash, password)
	if err != nil || !ok {
		s.limiter.fail(ip)
		s.limiter.fail("u:" + name)
		return "", nil, ErrBadCredentials
	}
	s.limiter.reset(ip)
	s.limiter.reset("u:" + name)
	token := randomToken()
	now := time.Now()
	if len(userAgent) > 200 {
		userAgent = userAgent[:200]
	}
	_, err = s.db.Exec(`INSERT INTO sessions(token_hash, user_id, kind, created_at, expires_at, user_agent)
		VALUES(?,?,?,?,?,?)`, hashToken(token), u.ID, kind, now.Unix(),
		now.Add(s.sessionTTL).Unix(), userAgent)
	if err != nil {
		return "", nil, err
	}
	return token, u, nil
}

// Logout deletes the session for a raw token.
func (s *Service) Logout(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, hashToken(token))
	return err
}

// UserByToken resolves a raw token to its user, or nil when invalid/expired.
func (s *Service) UserByToken(token string) *User {
	if token == "" {
		return nil
	}
	u := &User{}
	var exp int64
	err := s.db.QueryRow(`SELECT u.id, u.name, u.display_name, u.is_admin, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash=?`, hashToken(token)).
		Scan(&u.ID, &u.Name, &u.DisplayName, &u.IsAdmin, &exp)
	if err != nil || time.Now().Unix() > exp {
		return nil
	}
	return u
}

// PurgeExpired removes expired sessions; call periodically.
func (s *Service) PurgeExpired() {
	s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now().Unix())
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// ---- request context ------------------------------------------------------

type ctxKey struct{}

// WithUser stores the user in ctx.
func WithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// FromContext returns the current user or nil.
func FromContext(ctx context.Context) *User {
	u, _ := ctx.Value(ctxKey{}).(*User)
	return u
}

// TokenFromRequest reads the bearer token or the session cookie.
func TokenFromRequest(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie(CookieName); err == nil {
		return c.Value
	}
	return ""
}

// ---- rate limiter ---------------------------------------------------------

type attempt struct {
	failures    int
	first       time.Time
	lockedUntil time.Time
}

type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	lock   time.Duration
	m      map[string]*attempt
}

func newLimiter(max int, window, lock time.Duration) *limiter {
	if max <= 0 {
		max = 5
	}
	return &limiter{max: max, window: window, lock: lock, m: map[string]*attempt{}}
}

func (l *limiter) locked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.m[key]
	return a != nil && time.Now().Before(a.lockedUntil)
}

func (l *limiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	a := l.m[key]
	if a == nil || now.Sub(a.first) > l.window {
		a = &attempt{first: now}
		l.m[key] = a
	}
	a.failures++
	if a.failures >= l.max {
		a.lockedUntil = now.Add(l.lock)
		a.failures = 0
		a.first = now
	}
	// opportunistic cleanup so the map cannot grow forever
	if len(l.m) > 10000 {
		for k, v := range l.m {
			if now.Sub(v.first) > l.window && now.After(v.lockedUntil) {
				delete(l.m, k)
			}
		}
	}
}

func (l *limiter) reset(key string) {
	l.mu.Lock()
	delete(l.m, key)
	l.mu.Unlock()
}
