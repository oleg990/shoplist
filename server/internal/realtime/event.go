// Package realtime рассылает участникам списка уведомления «в списке что-то изменилось».
// Сами данные клиент забирает обычным запросом (GET .../items?since=N), поэтому онлайн и офлайн работают одинаково.
package realtime

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"shoplist/server/internal/store"
)

const channel = "list_changes"

// Виды изменений.
const (
	KindItems   = "items"   // позиции (в Version номер версии списка)
	KindMembers = "members" // состав участников
	KindList    = "list"    // сам список: создан, переименован, удалён
)

// Event описывает изменение списка. AlsoUser получит уведомление, даже если уже не участник
// (например, его только что убрали из списка).
type Event struct {
	ListID   uuid.UUID  `json:"list_id"`
	Kind     string     `json:"kind"`
	Version  int64      `json:"version,omitempty"`
	AlsoUser *uuid.UUID `json:"also_user,omitempty"`
}

// Notify отправляет событие. В транзакции оно уйдёт только после её коммита.
func Notify(ctx context.Context, q *store.Queries, ev Event) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	return q.Notify(ctx, string(b))
}
