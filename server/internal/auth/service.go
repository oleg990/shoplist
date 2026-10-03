// Package auth реализует регистрацию и вход по логину и паролю, коды восстановления, JWT и refresh-токены.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"shoplist/server/internal/store"
)

const (
	AccessTokenTTL  = 15 * time.Minute
	RefreshTokenTTL = 90 * 24 * time.Hour

	MinPasswordLen = 8
	MaxPasswordLen = 128
	// После MaxFailedLogins неудач подряд вход в аккаунт блокируется на LockDuration.
	MaxFailedLogins = 10
	LockDuration    = 15 * time.Minute
	RecoveryCodes   = 8
)

var (
	ErrInvalidUsername = errors.New("username must be 3-32 characters: letters, digits, '.', '_' or '-'")
	ErrWeakPassword    = errors.New("password must be 8-128 characters")
	ErrUsernameTaken   = errors.New("username is taken")
	ErrInvalidLogin    = errors.New("invalid username or password")
	ErrInvalidRecovery = errors.New("invalid username or recovery code")
	ErrLocked          = errors.New("too many failed attempts, try again later")
	ErrInvalidToken    = errors.New("invalid token")
)

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{3,32}$`)

type User struct {
	ID       uuid.UUID `json:"id"`
	Username string    `json:"username"`
	Name     string    `json:"name"`
}

type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	User         User   `json:"user"`
	// RecoveryCodes возвращаются один раз: при регистрации и при выпуске новых кодов.
	RecoveryCodes []string `json:"recovery_codes,omitempty"`
}

type Service struct {
	pool      *pgxpool.Pool
	q         *store.Queries
	secret    []byte
	accessTTL time.Duration
}

func NewService(pool *pgxpool.Pool, secret string) *Service {
	return &Service{pool: pool, q: store.New(pool), secret: []byte(secret), accessTTL: AccessTokenTTL}
}

func validateUsername(u string) error {
	if !usernameRe.MatchString(u) {
		return ErrInvalidUsername
	}
	return nil
}

func validatePassword(p string) error {
	if n := utf8.RuneCountInString(p); n < MinPasswordLen || n > MaxPasswordLen {
		return ErrWeakPassword
	}
	return nil
}

// Register создаёт аккаунт и сразу выдаёт токены и коды восстановления (показываются один раз).
func (s *Service) Register(ctx context.Context, username, password, name string) (Tokens, error) {
	username = strings.TrimSpace(username)
	if err := validateUsername(username); err != nil {
		return Tokens{}, err
	}
	if err := validatePassword(password); err != nil {
		return Tokens{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return Tokens{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Tokens{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.q.WithTx(tx)

	u, err := q.CreateUser(ctx, store.CreateUserParams{Username: username, Name: strings.TrimSpace(name)})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Tokens{}, ErrUsernameTaken
		}
		return Tokens{}, err
	}
	if err := q.CreateCredentials(ctx, store.CreateCredentialsParams{UserID: u.ID, PasswordHash: hash}); err != nil {
		return Tokens{}, err
	}
	codes, err := s.newRecoveryCodes(ctx, q, u.ID)
	if err != nil {
		return Tokens{}, err
	}
	tokens, err := s.issue(ctx, q, User{ID: u.ID, Username: u.Username, Name: u.Name})
	if err != nil {
		return Tokens{}, err
	}
	tokens.RecoveryCodes = codes
	return tokens, tx.Commit(ctx)
}

// LockedError сообщает, на сколько аккаунт заблокирован после серии неудач.
type LockedError struct{ RetryAfter time.Duration }

func (e LockedError) Error() string { return ErrLocked.Error() }
func (e LockedError) Unwrap() error { return ErrLocked }

// Login проверяет пароль. Неверный логин и неверный пароль неразличимы ни по ответу, ни по времени.
func (s *Service) Login(ctx context.Context, username, password string) (Tokens, error) {
	username = strings.TrimSpace(username)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Tokens{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.q.WithTx(tx)

	u, err := q.GetUserByUsername(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = verifyPassword(password, dummyHash)
		return Tokens{}, ErrInvalidLogin
	} else if err != nil {
		return Tokens{}, err
	}
	cred, err := q.GetCredentialsForUpdate(ctx, u.ID)
	if err != nil {
		return Tokens{}, err
	}
	if cred.LockedUntil.Valid && cred.LockedUntil.Time.After(time.Now()) {
		return Tokens{}, LockedError{RetryAfter: time.Until(cred.LockedUntil.Time)}
	}
	ok, err := verifyPassword(password, cred.PasswordHash)
	if err != nil {
		return Tokens{}, err
	}
	if !ok {
		// Счётчик неудач должен сохраниться, поэтому коммитим.
		if err := q.RecordLoginFailure(ctx, store.RecordLoginFailureParams{UserID: u.ID, MaxAttempts: MaxFailedLogins, LockSecs: LockDuration.Seconds()}); err != nil {
			return Tokens{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Tokens{}, err
		}
		return Tokens{}, ErrInvalidLogin
	}
	if cred.FailedAttempts > 0 || cred.LockedUntil.Valid {
		if err := q.ResetLoginFailures(ctx, u.ID); err != nil {
			return Tokens{}, err
		}
	}
	tokens, err := s.issue(ctx, q, User{ID: u.ID, Username: u.Username, Name: u.Name})
	if err != nil {
		return Tokens{}, err
	}
	return tokens, tx.Commit(ctx)
}

// Recover задаёт новый пароль по одноразовому коду восстановления. Все прежние сессии завершаются.
// Неудачные попытки считаются так же, как неверные пароли при входе.
func (s *Service) Recover(ctx context.Context, username, code, newPassword string) (Tokens, error) {
	if err := validatePassword(newPassword); err != nil {
		return Tokens{}, err
	}
	username = strings.TrimSpace(username)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Tokens{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.q.WithTx(tx)

	u, err := q.GetUserByUsername(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = verifyPassword(code, dummyHash)
		return Tokens{}, ErrInvalidRecovery
	} else if err != nil {
		return Tokens{}, err
	}
	cred, err := q.GetCredentialsForUpdate(ctx, u.ID)
	if err != nil {
		return Tokens{}, err
	}
	if cred.LockedUntil.Valid && cred.LockedUntil.Time.After(time.Now()) {
		return Tokens{}, LockedError{RetryAfter: time.Until(cred.LockedUntil.Time)}
	}
	if _, err := q.UseRecoveryCode(ctx, store.UseRecoveryCodeParams{UserID: u.ID, CodeHash: s.hashRecovery(code)}); errors.Is(err, pgx.ErrNoRows) {
		if err := q.RecordLoginFailure(ctx, store.RecordLoginFailureParams{UserID: u.ID, MaxAttempts: MaxFailedLogins, LockSecs: LockDuration.Seconds()}); err != nil {
			return Tokens{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Tokens{}, err
		}
		return Tokens{}, ErrInvalidRecovery
	} else if err != nil {
		return Tokens{}, err
	}

	hash, err := hashPassword(newPassword)
	if err != nil {
		return Tokens{}, err
	}
	if err := q.SetPassword(ctx, store.SetPasswordParams{UserID: u.ID, PasswordHash: hash}); err != nil {
		return Tokens{}, err
	}
	if err := q.RevokeAllUserRefreshTokens(ctx, u.ID); err != nil {
		return Tokens{}, err
	}
	tokens, err := s.issue(ctx, q, User{ID: u.ID, Username: u.Username, Name: u.Name})
	if err != nil {
		return Tokens{}, err
	}
	return tokens, tx.Commit(ctx)
}

// ChangePassword меняет пароль, зная старый; остальные сессии завершаются, текущая получает новые токены.
func (s *Service) ChangePassword(ctx context.Context, userID uuid.UUID, oldPassword, newPassword string) (Tokens, error) {
	if err := validatePassword(newPassword); err != nil {
		return Tokens{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Tokens{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.q.WithTx(tx)

	cred, err := q.GetCredentialsForUpdate(ctx, userID)
	if err != nil {
		return Tokens{}, err
	}
	if cred.LockedUntil.Valid && cred.LockedUntil.Time.After(time.Now()) {
		return Tokens{}, LockedError{RetryAfter: time.Until(cred.LockedUntil.Time)}
	}
	ok, err := verifyPassword(oldPassword, cred.PasswordHash)
	if err != nil {
		return Tokens{}, err
	}
	if !ok {
		if err := q.RecordLoginFailure(ctx, store.RecordLoginFailureParams{UserID: userID, MaxAttempts: MaxFailedLogins, LockSecs: LockDuration.Seconds()}); err != nil {
			return Tokens{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Tokens{}, err
		}
		return Tokens{}, ErrInvalidLogin
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return Tokens{}, err
	}
	if err := q.SetPassword(ctx, store.SetPasswordParams{UserID: userID, PasswordHash: hash}); err != nil {
		return Tokens{}, err
	}
	if err := q.RevokeAllUserRefreshTokens(ctx, userID); err != nil {
		return Tokens{}, err
	}
	u, err := q.GetUser(ctx, userID)
	if err != nil {
		return Tokens{}, err
	}
	tokens, err := s.issue(ctx, q, User{ID: u.ID, Username: u.Username, Name: u.Name})
	if err != nil {
		return Tokens{}, err
	}
	return tokens, tx.Commit(ctx)
}

// NewRecoveryCodes выпускает новый набор кодов (старые перестают действовать); нужен текущий пароль.
func (s *Service) NewRecoveryCodes(ctx context.Context, userID uuid.UUID, password string) ([]string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.q.WithTx(tx)

	cred, err := q.GetCredentialsForUpdate(ctx, userID)
	if err != nil {
		return nil, err
	}
	ok, err := verifyPassword(password, cred.PasswordHash)
	if err != nil {
		return nil, err
	}
	if !ok {
		if err := q.RecordLoginFailure(ctx, store.RecordLoginFailureParams{UserID: userID, MaxAttempts: MaxFailedLogins, LockSecs: LockDuration.Seconds()}); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, ErrInvalidLogin
	}
	codes, err := s.newRecoveryCodes(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	return codes, tx.Commit(ctx)
}

// UnusedRecoveryCodes сообщает, сколько кодов восстановления осталось.
func (s *Service) UnusedRecoveryCodes(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := s.q.CountUnusedRecoveryCodes(ctx, userID)
	return int(n), err
}

// recoveryAlphabet без похожих символов (0/O, 1/I/L), чтобы код можно было переписать с бумаги.
const recoveryAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// newRecoveryCodes заменяет все коды пользователя новыми и возвращает их открытым текстом (один раз).
func (s *Service) newRecoveryCodes(ctx context.Context, q *store.Queries, userID uuid.UUID) ([]string, error) {
	if err := q.DeleteRecoveryCodes(ctx, userID); err != nil {
		return nil, err
	}
	codes := make([]string, 0, RecoveryCodes)
	for len(codes) < RecoveryCodes {
		raw := make([]byte, 10)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		for i, b := range raw {
			raw[i] = recoveryAlphabet[int(b)%len(recoveryAlphabet)]
		}
		code := string(raw[:5]) + "-" + string(raw[5:])
		if err := q.InsertRecoveryCode(ctx, store.InsertRecoveryCodeParams{UserID: userID, CodeHash: s.hashRecovery(code)}); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, nil
}

// hashRecovery: HMAC от кода без дефиса и регистра (коды вводят руками).
func (s *Service) hashRecovery(code string) string {
	norm := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte("recovery:" + norm))
	return hex.EncodeToString(m.Sum(nil))
}

// Refresh меняет refresh-токен на новую пару. Повторное использование старого токена
// считается кражей: все токены пользователя отзываются.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (Tokens, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Tokens{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.q.WithTx(tx)

	row, err := q.GetRefreshTokenForUpdate(ctx, hashToken(refreshToken))
	if errors.Is(err, pgx.ErrNoRows) {
		return Tokens{}, ErrInvalidToken
	} else if err != nil {
		return Tokens{}, err
	}
	if row.RevokedAt.Valid {
		if err := q.RevokeAllUserRefreshTokens(ctx, row.UserID); err != nil {
			return Tokens{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Tokens{}, err
		}
		return Tokens{}, ErrInvalidToken
	}
	if time.Now().After(row.ExpiresAt.Time) {
		return Tokens{}, ErrInvalidToken
	}

	u, err := q.GetUser(ctx, row.UserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tokens{}, ErrInvalidToken
	} else if err != nil {
		return Tokens{}, err
	}
	if err := q.RevokeRefreshToken(ctx, row.ID); err != nil {
		return Tokens{}, err
	}
	tokens, err := s.issue(ctx, q, User{ID: u.ID, Username: u.Username, Name: u.Name})
	if err != nil {
		return Tokens{}, err
	}
	return tokens, tx.Commit(ctx)
}

// Logout отзывает refresh-токен (повторный вызов безопасен).
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	return s.q.RevokeRefreshTokenByHash(ctx, hashToken(refreshToken))
}

// SetAccessTTL меняет срок жизни access-токена (нужно тестам).
func (s *Service) SetAccessTTL(d time.Duration) { s.accessTTL = d }

// ParseAccessToken возвращает id пользователя из access-токена.
func (s *Service) ParseAccessToken(token string) (uuid.UUID, error) {
	id, _, err := s.ParseAccessTokenExp(token)
	return id, err
}

// ParseAccessTokenExp возвращает id пользователя и момент, когда токен перестанет действовать.
func (s *Service) ParseAccessTokenExp(token string) (uuid.UUID, time.Time, error) {
	claims := jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil {
		return uuid.Nil, time.Time{}, ErrInvalidToken
	}
	id, err := uuid.Parse(claims.Subject)
	if err != nil || claims.ExpiresAt == nil {
		return uuid.Nil, time.Time{}, ErrInvalidToken
	}
	return id, claims.ExpiresAt.Time, nil
}

func (s *Service) GetUser(ctx context.Context, id uuid.UUID) (User, error) {
	u, err := s.q.GetUser(ctx, id)
	if err != nil {
		return User{}, err
	}
	return User{ID: u.ID, Username: u.Username, Name: u.Name}, nil
}

func (s *Service) UpdateName(ctx context.Context, id uuid.UUID, name string) (User, error) {
	u, err := s.q.UpdateUserName(ctx, store.UpdateUserNameParams{ID: id, Name: strings.TrimSpace(name)})
	if err != nil {
		return User{}, err
	}
	return User{ID: u.ID, Username: u.Username, Name: u.Name}, nil
}

// DeleteAccount удаляет аккаунт (требование App Store и Google Play).
// Общие списки переходят другому участнику, остальные данные пользователя удаляются.
func (s *Service) DeleteAccount(ctx context.Context, id uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.q.WithTx(tx)

	if err := q.TransferOwnedLists(ctx, id); err != nil {
		return err
	}
	if err := q.DeleteUser(ctx, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) issue(ctx context.Context, q *store.Queries, u User) (Tokens, error) {
	now := time.Now()
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   u.ID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
	}).SignedString(s.secret)
	if err != nil {
		return Tokens{}, err
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Tokens{}, err
	}
	refresh := base64.RawURLEncoding.EncodeToString(raw)
	if err := q.CreateRefreshToken(ctx, store.CreateRefreshTokenParams{
		UserID:    u.ID,
		TokenHash: hashToken(refresh),
		ExpiresAt: pgtype.Timestamptz{Time: now.Add(RefreshTokenTTL), Valid: true},
	}); err != nil {
		return Tokens{}, err
	}
	return Tokens{AccessToken: access, RefreshToken: refresh, ExpiresIn: int(s.accessTTL.Seconds()), User: u}, nil
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}
