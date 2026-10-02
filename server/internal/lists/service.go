// Package lists реализует списки покупок, участников и приглашения по коду.
package lists

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"shoplist/server/internal/realtime"
	"shoplist/server/internal/store"
)

const (
	RoleOwner  = "owner"
	RoleEditor = "editor"

	MaxMembers        = 50
	DefaultInviteTTL  = 7 * 24 * time.Hour
	MaxInviteTTL      = 30 * 24 * time.Hour
	DefaultInviteUses = 10
	MaxInviteUses     = 50

	// Алфавит без похожих символов (0/O, 1/I/L), чтобы код легко диктовать.
	inviteAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	inviteLen      = 8
)

var (
	// ErrNotFound: списка нет или пользователь в нём не участвует (не раскрываем, что именно).
	ErrNotFound = errors.New("list not found")
	// ErrForbidden: пользователь участник, но действие доступно только владельцу.
	ErrForbidden        = errors.New("owner only")
	ErrInvalidTitle     = errors.New("title must be 1..100 characters")
	ErrInviteInvalid    = errors.New("invite is invalid, expired or used up")
	ErrListFull         = errors.New("list has too many members")
	ErrOwnerCannotLeave = errors.New("owner cannot leave the list")
)

type List struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	OwnerID     uuid.UUID `json:"owner_id"`
	Role        string    `json:"role"`
	MemberCount int       `json:"member_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Member struct {
	UserID   uuid.UUID `json:"user_id"`
	Name     string    `json:"name"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

type Invite struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
	MaxUses   int       `json:"max_uses"`
	Uses      int       `json:"uses"`
}

type Service struct {
	pool *pgxpool.Pool
	q    *store.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: store.New(pool)}
}

func validTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if n := len([]rune(title)); n == 0 || n > 100 {
		return "", ErrInvalidTitle
	}
	return title, nil
}

func (s *Service) Create(ctx context.Context, userID uuid.UUID, title string) (List, error) {
	title, err := validTitle(title)
	if err != nil {
		return List{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return List{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.q.WithTx(tx)

	l, err := q.CreateList(ctx, store.CreateListParams{Title: title, OwnerID: userID})
	if err != nil {
		return List{}, err
	}
	if err := q.AddListMember(ctx, store.AddListMemberParams{ListID: l.ID, UserID: userID, Role: RoleOwner}); err != nil {
		return List{}, err
	}
	// Другие устройства создателя узнают о новом списке.
	if err := realtime.Notify(ctx, q, realtime.Event{ListID: l.ID, Kind: realtime.KindList}); err != nil {
		return List{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return List{}, err
	}
	return List{ID: l.ID, Title: l.Title, OwnerID: l.OwnerID, Role: RoleOwner, MemberCount: 1,
		CreatedAt: l.CreatedAt.Time, UpdatedAt: l.UpdatedAt.Time}, nil
}

func (s *Service) ListForUser(ctx context.Context, userID uuid.UUID) ([]List, error) {
	rows, err := s.q.ListListsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]List, 0, len(rows))
	for _, r := range rows {
		out = append(out, List{ID: r.ID, Title: r.Title, OwnerID: r.OwnerID, Role: r.Role, MemberCount: int(r.MemberCount),
			CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time})
	}
	return out, nil
}

// Get возвращает список, если пользователь в нём участвует.
func (s *Service) Get(ctx context.Context, userID, listID uuid.UUID) (List, error) {
	return get(ctx, s.q, userID, listID)
}

func get(ctx context.Context, q *store.Queries, userID, listID uuid.UUID) (List, error) {
	r, err := q.GetListForMember(ctx, store.GetListForMemberParams{ListID: listID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return List{}, ErrNotFound
	} else if err != nil {
		return List{}, err
	}
	return List{ID: r.ID, Title: r.Title, OwnerID: r.OwnerID, Role: r.Role, MemberCount: int(r.MemberCount),
		CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time}, nil
}

func (s *Service) requireOwner(ctx context.Context, userID, listID uuid.UUID) (List, error) {
	l, err := s.Get(ctx, userID, listID)
	if err != nil {
		return List{}, err
	}
	if l.Role != RoleOwner {
		return List{}, ErrForbidden
	}
	return l, nil
}

func (s *Service) Rename(ctx context.Context, userID, listID uuid.UUID, title string) (List, error) {
	title, err := validTitle(title)
	if err != nil {
		return List{}, err
	}
	if _, err := s.requireOwner(ctx, userID, listID); err != nil {
		return List{}, err
	}
	if err := s.q.RenameList(ctx, store.RenameListParams{ID: listID, Title: title}); err != nil {
		return List{}, err
	}
	s.notify(ctx, realtime.Event{ListID: listID, Kind: realtime.KindList})
	return s.Get(ctx, userID, listID)
}

func (s *Service) Delete(ctx context.Context, userID, listID uuid.UUID) error {
	if _, err := s.requireOwner(ctx, userID, listID); err != nil {
		return err
	}
	if err := s.q.SoftDeleteList(ctx, listID); err != nil {
		return err
	}
	s.notify(ctx, realtime.Event{ListID: listID, Kind: realtime.KindList})
	return nil
}

// notify отправляет уведомление после уже выполненной записи. Ошибка не мешает операции:
// клиенты всё равно догонят изменения при следующем запросе.
func (s *Service) notify(ctx context.Context, ev realtime.Event) {
	_ = realtime.Notify(ctx, s.q, ev)
}

func (s *Service) Members(ctx context.Context, userID, listID uuid.UUID) ([]Member, error) {
	if _, err := s.Get(ctx, userID, listID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListMembers(ctx, listID)
	if err != nil {
		return nil, err
	}
	out := make([]Member, 0, len(rows))
	for _, r := range rows {
		name := strings.TrimSpace(r.Name)
		if name == "" {
			name = maskEmail(r.Email)
		}
		out = append(out, Member{UserID: r.UserID, Name: name, Role: r.Role, JoinedAt: r.JoinedAt.Time})
	}
	return out, nil
}

// RemoveMember: владелец убирает любого участника, участник может выйти сам. Владелец выйти не может.
func (s *Service) RemoveMember(ctx context.Context, actorID, listID, targetID uuid.UUID) error {
	l, err := s.Get(ctx, actorID, listID)
	if err != nil {
		return err
	}
	if actorID != targetID && l.Role != RoleOwner {
		return ErrForbidden
	}
	if targetID == l.OwnerID {
		return ErrOwnerCannotLeave
	}
	n, err := s.q.RemoveListMember(ctx, store.RemoveListMemberParams{ListID: listID, UserID: targetID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	if err := s.q.TouchList(ctx, listID); err != nil {
		return err
	}
	// Убранный участник уже не в списке, но должен узнать об этом.
	s.notify(ctx, realtime.Event{ListID: listID, Kind: realtime.KindMembers, AlsoUser: &targetID})
	return nil
}

// CreateInvite: приглашать может любой участник списка.
func (s *Service) CreateInvite(ctx context.Context, userID, listID uuid.UUID, ttl time.Duration, maxUses int) (Invite, error) {
	if _, err := s.Get(ctx, userID, listID); err != nil {
		return Invite{}, err
	}
	if ttl <= 0 {
		ttl = DefaultInviteTTL
	}
	ttl = min(ttl, MaxInviteTTL)
	if maxUses <= 0 {
		maxUses = DefaultInviteUses
	}
	maxUses = min(maxUses, MaxInviteUses)

	code, err := newInviteCode()
	if err != nil {
		return Invite{}, err
	}
	r, err := s.q.CreateInvite(ctx, store.CreateInviteParams{
		ListID: listID, Code: code, CreatedBy: userID,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(ttl), Valid: true},
		MaxUses:   int32(maxUses),
	})
	if err != nil {
		return Invite{}, err
	}
	return Invite{Code: r.Code, ExpiresAt: r.ExpiresAt.Time, MaxUses: int(r.MaxUses), Uses: int(r.Uses)}, nil
}

// AcceptInvite добавляет пользователя в список по коду. Повторное принятие участником безопасно и не тратит приглашение.
func (s *Service) AcceptInvite(ctx context.Context, userID uuid.UUID, code string) (List, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return List{}, ErrInviteInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return List{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.q.WithTx(tx)

	inv, err := q.GetInviteByCodeForUpdate(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return List{}, ErrInviteInvalid
	} else if err != nil {
		return List{}, err
	}

	already, err := q.IsListMember(ctx, store.IsListMemberParams{ListID: inv.ListID, UserID: userID})
	if err != nil {
		return List{}, err
	}
	if !already {
		if time.Now().After(inv.ExpiresAt.Time) || inv.Uses >= inv.MaxUses {
			return List{}, ErrInviteInvalid
		}
		n, err := q.CountListMembers(ctx, inv.ListID)
		if err != nil {
			return List{}, err
		}
		if n >= MaxMembers {
			return List{}, ErrListFull
		}
		if err := q.AddListMember(ctx, store.AddListMemberParams{ListID: inv.ListID, UserID: userID, Role: RoleEditor}); err != nil {
			return List{}, err
		}
		if err := q.IncrementInviteUses(ctx, inv.ID); err != nil {
			return List{}, err
		}
		if err := q.TouchList(ctx, inv.ListID); err != nil {
			return List{}, err
		}
		if err := realtime.Notify(ctx, q, realtime.Event{ListID: inv.ListID, Kind: realtime.KindMembers}); err != nil {
			return List{}, err
		}
	}
	l, err := get(ctx, q, userID, inv.ListID)
	if err != nil {
		return List{}, err
	}
	return l, tx.Commit(ctx)
}

func newInviteCode() (string, error) {
	// Отбрасываем байты >= limit, чтобы символы алфавита выпадали равновероятно.
	limit := byte(256 - 256%len(inviteAlphabet))
	out := make([]byte, 0, inviteLen)
	buf := make([]byte, inviteLen*2)
	for len(out) < inviteLen {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if b < limit && len(out) < inviteLen {
				out = append(out, inviteAlphabet[int(b)%len(inviteAlphabet)])
			}
		}
	}
	return string(out), nil
}

// maskEmail превращает "olga@gmail.com" в "o***@gmail.com": участники видят друг друга, но не чужие адреса.
func maskEmail(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" {
		return "***"
	}
	r := []rune(local)
	return string(r[0]) + "***@" + domain
}
