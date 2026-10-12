package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"golang.org/x/crypto/argon2"
	"strings"
	"time"
	"unicode/utf8"
)

func normalizeUsername(value string) string {
	b := []byte(value)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
func validUsername(s string) bool {
	if len(s) < 3 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func validName(s string) bool {
	return utf8.ValidString(s) && strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= 80
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func digest(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }
func csrf(token string) string {
	return base64.RawURLEncoding.EncodeToString(digest("family-location/csrf/v1:" + token))
}
func (s *Service) acquireHash() error {
	select {
	case s.hashSlots <- struct{}{}:
		return nil
	default:
		return ErrRateLimited
	}
}
func passwordHash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	return "$argon2id$v=19$m=65536,t=1,p=4$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}
func checkPassword(password, encoded string) bool {
	// Unknown users and malformed stored hashes still perform the full hash cost.
	salt := make([]byte, 16)
	expected := make([]byte, 32)
	valid := false
	parts := strings.Split(encoded, "$")
	if len(parts) == 6 && parts[1] == "argon2id" && parts[2] == "v=19" && parts[3] == "m=65536,t=1,p=4" {
		a, e1 := base64.RawStdEncoding.DecodeString(parts[4])
		b, e2 := base64.RawStdEncoding.DecodeString(parts[5])
		if e1 == nil && e2 == nil && len(a) == 16 && len(b) == 32 {
			salt = a
			expected = b
			valid = true
		}
	}
	key := argon2.IDKey([]byte(password), salt, 1, 64*1024, 4, 32)
	return subtle.ConstantTimeCompare(key, expected) == 1 && valid
}
func (s *Service) CreateAdmin(ctx context.Context, username, password, family string) (Admin, error) {
	username = normalizeUsername(username)
	if !validUsername(username) || len(password) < 12 || len(password) > 128 || !validName(family) {
		return Admin{}, ErrInvalidInput
	}
	if err := s.acquireHash(); err != nil {
		return Admin{}, err
	}
	hash, err := passwordHash(password)
	<-s.hashSlots
	if err != nil {
		return Admin{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Admin{}, fmt.Errorf("begin admin creation: %w", err)
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE username=?", username).Scan(&exists); err != nil {
		return Admin{}, fmt.Errorf("find admin: %w", err)
	}
	if exists > 0 {
		return Admin{}, ErrConflict
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO families(name,created_at) VALUES (?,?)", family, s.now().Unix())
	if err != nil {
		return Admin{}, fmt.Errorf("create family: %w", err)
	}
	familyID, err := result.LastInsertId()
	if err != nil {
		return Admin{}, fmt.Errorf("family ID: %w", err)
	}
	result, err = tx.ExecContext(ctx, "INSERT INTO users(family_id,username,password_hash,created_at) VALUES (?,?,?,?)", familyID, username, hash, s.now().Unix())
	if err != nil {
		return Admin{}, fmt.Errorf("create admin: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Admin{}, fmt.Errorf("admin ID: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Admin{}, fmt.Errorf("commit admin: %w", err)
	}
	return Admin{ID: id, FamilyID: familyID, Username: username}, nil
}
func (s *Service) Login(ctx context.Context, username, password, ip string) (Session, error) {
	username = normalizeUsername(username)
	if err := s.limit(ctx, limitRule{"login-user:" + username, 5}, limitRule{"login-ip:" + ip, 5}); err != nil {
		return Session{}, err
	}
	if err := s.acquireHash(); err != nil {
		return Session{}, err
	}
	var a Admin
	var encoded string
	err := s.db.QueryRowContext(ctx, "SELECT id,family_id,username,password_hash FROM users WHERE username=?", username).Scan(&a.ID, &a.FamilyID, &a.Username, &encoded)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		<-s.hashSlots
		return Session{}, fmt.Errorf("load login: %w", err)
	}
	inputValid := validUsername(username) && len(password) >= 12 && len(password) <= 128
	if !inputValid {
		password = "invalid password input"
	}
	valid := checkPassword(password, encoded)
	<-s.hashSlots
	if !valid || !inputValid {
		return Session{}, ErrInvalidCredentials
	}
	token, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	now := s.now()
	expires := now.Add(24 * time.Hour).Truncate(time.Second)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, fmt.Errorf("begin session: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM web_sessions WHERE expires_at<=?", now.Unix()); err != nil {
		return Session{}, fmt.Errorf("clean sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO web_sessions(token_hash,user_id,expires_at) VALUES (?,?,?)", digest(token), a.ID, expires.Unix()); err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Session{}, fmt.Errorf("commit session: %w", err)
	}
	return Session{Token: token, CSRFToken: csrf(token), ExpiresAt: expires, User: a}, nil
}
func (s *Service) AuthenticateSession(ctx context.Context, token string) (Session, error) {
	if len(token) != 43 {
		return Session{}, ErrUnauthorized
	}
	var session Session
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.family_id,u.username,w.expires_at FROM web_sessions w JOIN users u ON w.user_id=u.id WHERE w.token_hash=? AND w.expires_at>?`, digest(token), s.now().Unix()).Scan(&session.User.ID, &session.User.FamilyID, &session.User.Username, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrUnauthorized
	}
	if err != nil {
		return Session{}, fmt.Errorf("authenticate session: %w", err)
	}
	session.Token = token
	session.CSRFToken = csrf(token)
	session.ExpiresAt = time.Unix(expires, 0).UTC()
	return session, nil
}
func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM web_sessions WHERE token_hash=?", digest(token))
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}
