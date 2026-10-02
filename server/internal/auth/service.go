// Package auth реализует вход по одноразовому коду из письма, JWT и refresh-токены.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"shoplist/server/internal/store"
)

const (
	CodeTTL         = 10 * time.Minute
	CodeResendDelay = 60 * time.Second
	MaxCodeAttempts = 5
	AccessTokenTTL  = 15 * time.Minute
	RefreshTokenTTL = 90 * 24 * time.Hour
)

var (
	ErrInvalidEmail = errors.New("invalid email")
	ErrTooManyCodes = errors.New("code requested too recently")
	ErrInvalidCode  = errors.New("invalid or expired code")
	ErrInvalidToken = errors.New("invalid token")
)

type User struct {
	ID    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
}

type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	User         User   `json:"user"`
}

type Service struct {
	pool      *pgxpool.Pool
	q         *store.Queries
	mailer    Mailer
	secret    []byte
	accessTTL time.Duration
}

func NewService(pool *pgxpool.Pool, mailer Mailer, secret string) *Service {
	return &Service{pool: pool, q: store.New(pool), mailer: mailer, secret: []byte(secret), accessTTL: AccessTokenTTL}
}

// NormalizeEmail проверяет адрес и приводит его к нижнему регистру.
func NormalizeEmail(s string) (string, error) {
	s = strings.TrimSpace(s)
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || len(s) > 254 || !strings.Contains(s[strings.LastIndex(s, "@"):], ".") {
		return "", ErrInvalidEmail
	}
	return strings.ToLower(s), nil
}

// RequestCode создаёт и отправляет код. Не раскрывает, есть ли такой пользователь.
func (s *Service) RequestCode(ctx context.Context, rawEmail string) error {
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return err
	}

	last, err := s.q.LatestLoginCodeCreatedAt(ctx, email)
	if err == nil && time.Since(last.Time) < CodeResendDelay {
		return ErrTooManyCodes
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	code, err := newCode()
	if err != nil {
		return err
	}
	if err := s.q.CreateLoginCode(ctx, store.CreateLoginCodeParams{
		Email:     email,
		CodeHash:  s.hashCode(email, code),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(CodeTTL), Valid: true},
	}); err != nil {
		return err
	}
	_ = s.q.DeleteExpiredLoginCodes(ctx)

	if err := s.mailer.SendLoginCode(ctx, email, code); err != nil {
		return fmt.Errorf("send code: %w", err)
	}
	return nil
}

// VerifyCode проверяет код и выдаёт токены. Пользователь создаётся при первом входе.
func (s *Service) VerifyCode(ctx context.Context, rawEmail, code string) (Tokens, error) {
	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return Tokens{}, ErrInvalidEmail
	}
	code = strings.TrimSpace(code)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Tokens{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.q.WithTx(tx)

	row, err := q.GetActiveLoginCodeForUpdate(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tokens{}, ErrInvalidCode
	} else if err != nil {
		return Tokens{}, err
	}
	if row.Attempts >= MaxCodeAttempts {
		return Tokens{}, ErrInvalidCode
	}
	if !hmac.Equal([]byte(row.CodeHash), []byte(s.hashCode(email, code))) {
		// Счётчик попыток должен сохраниться, поэтому коммитим.
		if err := q.IncrementLoginCodeAttempts(ctx, row.ID); err != nil {
			return Tokens{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Tokens{}, err
		}
		return Tokens{}, ErrInvalidCode
	}

	if err := q.DeleteLoginCodesByEmail(ctx, email); err != nil {
		return Tokens{}, err
	}
	u, err := q.UpsertUserByEmail(ctx, email)
	if err != nil {
		return Tokens{}, err
	}
	if err := q.InsertEmailIdentity(ctx, store.InsertEmailIdentityParams{UserID: u.ID, Email: email}); err != nil {
		return Tokens{}, err
	}
	tokens, err := s.issue(ctx, q, User{ID: u.ID, Email: u.Email, Name: u.Name})
	if err != nil {
		return Tokens{}, err
	}
	return tokens, tx.Commit(ctx)
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
	tokens, err := s.issue(ctx, q, User{ID: u.ID, Email: u.Email, Name: u.Name})
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
	return User{ID: u.ID, Email: u.Email, Name: u.Name}, nil
}

func (s *Service) UpdateName(ctx context.Context, id uuid.UUID, name string) (User, error) {
	u, err := s.q.UpdateUserName(ctx, store.UpdateUserNameParams{ID: id, Name: strings.TrimSpace(name)})
	if err != nil {
		return User{}, err
	}
	return User{ID: u.ID, Email: u.Email, Name: u.Name}, nil
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

func newCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func (s *Service) hashCode(email, code string) string {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(email + ":" + code))
	return hex.EncodeToString(m.Sum(nil))
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}
