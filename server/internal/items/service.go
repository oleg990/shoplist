// Package items реализует позиции списка покупок: добавление, изменение, отметку «куплено» и очистку купленного.
package items

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"shoplist/server/internal/store"
)

const (
	maxNameLen  = 100
	maxQuantity = 9_999_999.0 // numeric(10,3)
	maxPrice    = 9_999_999_999.0
)

var (
	// ErrNotFound: списка нет, пользователь в нём не участвует или позиции нет.
	ErrNotFound = errors.New("not found")
	// ErrConflict: позиция с таким id уже есть в другом списке или удалена.
	ErrConflict = errors.New("item id is taken by another list or was deleted")
	// ErrInvalid: некорректные данные. Текст безопасно показывать клиенту.
	ErrInvalid = errors.New("invalid item")
)

func invalid(msg string) error { return errors.Join(ErrInvalid, errors.New(msg)) }

// Message возвращает текст ошибки валидации без служебного префикса.
func Message(err error) string {
	if errors.Is(err, ErrInvalid) {
		if parts := strings.SplitN(err.Error(), "\n", 2); len(parts) == 2 {
			return parts[1]
		}
	}
	return err.Error()
}

type Item struct {
	ID            uuid.UUID  `json:"id"`
	ListID        uuid.UUID  `json:"list_id"`
	CatalogItemID *int32     `json:"catalog_item_id"`
	Name          string     `json:"name"`
	Quantity      *float64   `json:"quantity"`
	Unit          *string    `json:"unit"`
	Price         *float64   `json:"price"`
	CategoryID    *int32     `json:"category_id"`
	IsBought      bool       `json:"is_bought"`
	BoughtBy      *uuid.UUID `json:"bought_by"`
	BoughtAt      *time.Time `json:"bought_at"`
	Position      int32      `json:"position"`
	Version       int64      `json:"version"`
	UpdatedAt     time.Time  `json:"updated_at"`
	// Deleted приходит только при запросе изменений (since > 0), чтобы клиент убрал позицию у себя.
	Deleted bool `json:"deleted"`
}

// Input: все поля позиции для создания или полной замены.
type Input struct {
	CatalogItemID *int32
	Name          string
	Quantity      *float64
	Unit          *string
	Price         *float64
	CategoryID    *int32
	IsBought      bool
	Position      int32
}

// Optional различает «поля нет в JSON» и «поле равно null» (очистить значение).
type Optional[T any] struct {
	Set   bool
	Value *T
}

func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	o.Value = new(T)
	return unmarshal(b, o.Value)
}

// Patch: частичное изменение. Для name, is_bought и position значение null не допускается.
type Patch struct {
	Name       Optional[string]  `json:"name"`
	Quantity   Optional[float64] `json:"quantity"`
	Unit       Optional[string]  `json:"unit"`
	Price      Optional[float64] `json:"price"`
	CategoryID Optional[int32]   `json:"category_id"`
	Position   Optional[int32]   `json:"position"`
	IsBought   Optional[bool]    `json:"is_bought"`
}

type Service struct {
	pool *pgxpool.Pool
	q    *store.Queries
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: store.New(pool)}
}

// List возвращает позиции списка. С since > 0 отдаёт только изменения новее этой версии, включая удалённые.
// Cursor нужно передать в следующий запрос как since.
func (s *Service) List(ctx context.Context, userID, listID uuid.UUID, since int64) ([]Item, int64, error) {
	if err := s.requireMember(ctx, s.q, userID, listID); err != nil {
		return nil, 0, err
	}
	rows, err := s.q.ListItems(ctx, store.ListItemsParams{ListID: listID, Since: since, IncludeDeleted: since > 0})
	if err != nil {
		return nil, 0, err
	}
	out := make([]Item, 0, len(rows))
	cursor := since
	for _, r := range rows {
		out = append(out, toItem(r))
		cursor = max(cursor, r.Version)
	}
	return out, cursor, nil
}

// Put создаёт позицию с id от клиента или заменяет её. Повторный вызов безопасен (офлайн-клиент может повторять).
func (s *Service) Put(ctx context.Context, userID, listID, itemID uuid.UUID, in Input) (Item, error) {
	if err := in.validate(); err != nil {
		return Item{}, err
	}
	var item Item
	err := s.inListTx(ctx, userID, listID, func(q *store.Queries, version int64) error {
		row, err := q.UpsertItem(ctx, store.UpsertItemParams{
			ID: itemID, ListID: listID,
			CatalogItemID: int4(in.CatalogItemID), Name: strings.TrimSpace(in.Name),
			Quantity: numeric(in.Quantity), Unit: text(in.Unit), Price: numeric(in.Price),
			CategoryID: int4(in.CategoryID), IsBought: in.IsBought, Actor: userID,
			Position: in.Position, Version: version,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrConflict
		} else if err != nil {
			return mapFK(err)
		}
		item = toItem(store.ListItemsRow(row))
		return nil
	})
	return item, err
}

func (s *Service) Patch(ctx context.Context, userID, listID, itemID uuid.UUID, p Patch) (Item, error) {
	if err := p.validate(); err != nil {
		return Item{}, err
	}
	var item Item
	err := s.inListTx(ctx, userID, listID, func(q *store.Queries, version int64) error {
		args := store.PatchItemParams{ID: itemID, ListID: listID, Actor: userID, Version: version}
		if p.Name.Set {
			args.SetName, args.Name = true, strings.TrimSpace(*p.Name.Value)
		}
		if p.Quantity.Set {
			args.SetQuantity, args.Quantity = true, numeric(p.Quantity.Value)
		}
		if p.Unit.Set {
			args.SetUnit, args.Unit = true, text(p.Unit.Value)
		}
		if p.Price.Set {
			args.SetPrice, args.Price = true, numeric(p.Price.Value)
		}
		if p.CategoryID.Set {
			args.SetCategoryID, args.CategoryID = true, int4(p.CategoryID.Value)
		}
		if p.Position.Set {
			args.SetPosition, args.Position = true, *p.Position.Value
		}
		if p.IsBought.Set {
			args.SetBought, args.IsBought = true, *p.IsBought.Value
		}
		row, err := q.PatchItem(ctx, args)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return mapFK(err)
		}
		item = toItem(store.ListItemsRow(row))
		return nil
	})
	return item, err
}

// Delete удаляет позицию (мягко: версия растёт, клиенты увидят deleted).
func (s *Service) Delete(ctx context.Context, userID, listID, itemID uuid.UUID) error {
	return s.inListTx(ctx, userID, listID, func(q *store.Queries, version int64) error {
		n, err := q.SoftDeleteItem(ctx, store.SoftDeleteItemParams{ID: itemID, ListID: listID, Version: version})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ClearBought убирает купленные позиции из списка и сохраняет их в истории покупок.
func (s *Service) ClearBought(ctx context.Context, userID, listID uuid.UUID) (int, error) {
	var cleared int
	err := s.inListTx(ctx, userID, listID, func(q *store.Queries, version int64) error {
		if _, err := q.ArchiveBoughtItems(ctx, listID); err != nil {
			return err
		}
		n, err := q.SoftDeleteBoughtItems(ctx, store.SoftDeleteBoughtItemsParams{ListID: listID, Version: version})
		cleared = int(n)
		return err
	})
	return cleared, err
}

// inListTx проверяет, что пользователь участник списка, занимает следующую версию списка
// и выполняет fn в одной транзакции. Блокировка строки списка упорядочивает записи.
func (s *Service) inListTx(ctx context.Context, userID, listID uuid.UUID, fn func(q *store.Queries, version int64) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.q.WithTx(tx)

	if err := s.requireMember(ctx, q, userID, listID); err != nil {
		return err
	}
	version, err := q.NextItemVersion(ctx, listID)
	if err != nil {
		return err
	}
	if err := fn(q, version); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) requireMember(ctx context.Context, q *store.Queries, userID, listID uuid.UUID) error {
	ok, err := q.IsActiveListMember(ctx, store.IsActiveListMemberParams{ListID: listID, UserID: userID})
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

func (in Input) validate() error {
	if err := validName(in.Name); err != nil {
		return err
	}
	if err := validQuantity(in.Quantity); err != nil {
		return err
	}
	if err := validPrice(in.Price); err != nil {
		return err
	}
	if in.Position < 0 {
		return invalid("position must not be negative")
	}
	return nil
}

func (p Patch) validate() error {
	if p.Name.Set {
		if p.Name.Value == nil {
			return invalid("name cannot be null")
		}
		if err := validName(*p.Name.Value); err != nil {
			return err
		}
	}
	if p.IsBought.Set && p.IsBought.Value == nil {
		return invalid("is_bought cannot be null")
	}
	if p.Position.Set {
		if p.Position.Value == nil || *p.Position.Value < 0 {
			return invalid("position must be a non-negative number")
		}
	}
	if p.Quantity.Set {
		if err := validQuantity(p.Quantity.Value); err != nil {
			return err
		}
	}
	if p.Price.Set {
		if err := validPrice(p.Price.Value); err != nil {
			return err
		}
	}
	return nil
}

func validName(name string) error {
	if n := len([]rune(strings.TrimSpace(name))); n == 0 || n > maxNameLen {
		return invalid("name must be 1..100 characters")
	}
	return nil
}

func validQuantity(q *float64) error {
	if q != nil && (math.IsNaN(*q) || *q <= 0 || *q > maxQuantity) {
		return invalid("quantity must be greater than 0 and at most 9999999")
	}
	return nil
}

func validPrice(p *float64) error {
	if p != nil && (math.IsNaN(*p) || *p < 0 || *p > maxPrice) {
		return invalid("price must be between 0 and 9999999999")
	}
	return nil
}

// mapFK превращает нарушение внешнего ключа (неизвестная единица, категория, товар справочника) в ошибку клиента.
func mapFK(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return invalid("unknown unit, category or catalog item")
	}
	return err
}

func toItem(r store.ListItemsRow) Item {
	it := Item{
		ID: r.ID, ListID: r.ListID, Name: r.Name, IsBought: r.IsBought, Position: r.Position,
		Version: r.Version, UpdatedAt: r.UpdatedAt.Time, Deleted: r.Deleted,
		Quantity: float(r.Quantity), Price: float(r.Price),
	}
	if r.CatalogItemID.Valid {
		it.CatalogItemID = &r.CatalogItemID.Int32
	}
	if r.CategoryID.Valid {
		it.CategoryID = &r.CategoryID.Int32
	}
	if r.Unit.Valid {
		it.Unit = &r.Unit.String
	}
	if r.BoughtBy.Valid {
		it.BoughtBy = &r.BoughtBy.UUID
	}
	if r.BoughtAt.Valid {
		it.BoughtAt = &r.BoughtAt.Time
	}
	return it
}

func float(n pgtype.Numeric) *float64 {
	if !n.Valid {
		return nil
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	return &f.Float64
}

func numeric(f *float64) pgtype.Numeric {
	var n pgtype.Numeric
	if f != nil {
		// Через строку, чтобы не тащить двоичные хвосты float (0.1 + 0.2).
		_ = n.Scan(strconv.FormatFloat(*f, 'f', -1, 64))
	}
	return n
}

func text(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func int4(i *int32) pgtype.Int4 {
	if i == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *i, Valid: true}
}
