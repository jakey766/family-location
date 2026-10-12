package auth

import (
	"database/sql"
	"errors"
	"time"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrInvalidInput       = errors.New("invalid input")
	ErrConflict           = errors.New("already exists")
	ErrPairingInvalid     = errors.New("invalid pairing")
	ErrNotFound           = errors.New("not found")
	ErrRateLimited        = errors.New("rate limited")
)

type Admin struct {
	ID       int64  `json:"id"`
	FamilyID int64  `json:"family_id"`
	Username string `json:"username"`
}
type Session struct {
	Token     string    `json:"-"`
	CSRFToken string    `json:"csrf_token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      Admin     `json:"user"`
}
type Device struct {
	ID        string     `json:"id"`
	FamilyID  int64      `json:"family_id"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at"`
}
type Pairing struct {
	ID        string    `json:"pairing_id"`
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}
type PairedDevice struct {
	Device Device `json:"device"`
	Token  string `json:"device_token"`
}
type Service struct {
	db        *sql.DB
	now       func() time.Time
	hashSlots chan struct{}
}

// Bound expensive hashing across all Service instances in this process.
var processHashSlots = make(chan struct{}, 2)

func New(db *sql.DB) *Service { return &Service{db: db, now: time.Now, hashSlots: processHashSlots} }
