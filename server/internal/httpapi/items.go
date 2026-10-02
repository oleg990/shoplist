package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"shoplist/server/internal/items"
)

func (a *API) itemErr(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, items.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, items.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, items.ErrInvalid):
		writeError(w, http.StatusBadRequest, items.Message(err))
	default:
		a.log.Error(op, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func (a *API) listItems(w http.ResponseWriter, r *http.Request) {
	listID, ok := pathUUID(w, r, "listID")
	if !ok {
		return
	}
	var since int64
	if s := r.URL.Query().Get("since"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "since must be a non-negative integer")
			return
		}
		since = n
	}
	list, cursor, err := a.items.List(r.Context(), userID(r.Context()), listID, since)
	if err != nil {
		a.itemErr(w, "list items", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "cursor": cursor})
}

// putItem создаёт позицию с id от клиента или заменяет её целиком. Идемпотентен.
func (a *API) putItem(w http.ResponseWriter, r *http.Request) {
	listID, ok := pathUUID(w, r, "listID")
	if !ok {
		return
	}
	itemID, ok := pathUUID(w, r, "itemID")
	if !ok {
		return
	}
	var in struct {
		CatalogItemID *int32   `json:"catalog_item_id"`
		Name          string   `json:"name"`
		Quantity      *float64 `json:"quantity"`
		Unit          *string  `json:"unit"`
		Price         *float64 `json:"price"`
		CategoryID    *int32   `json:"category_id"`
		IsBought      bool     `json:"is_bought"`
		Position      int32    `json:"position"`
	}
	if !decode(w, r, &in) {
		return
	}
	item, err := a.items.Put(r.Context(), userID(r.Context()), listID, itemID, items.Input{
		CatalogItemID: in.CatalogItemID, Name: in.Name, Quantity: in.Quantity, Unit: in.Unit,
		Price: in.Price, CategoryID: in.CategoryID, IsBought: in.IsBought, Position: in.Position,
	})
	if err != nil {
		a.itemErr(w, "put item", err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) patchItem(w http.ResponseWriter, r *http.Request) {
	listID, ok := pathUUID(w, r, "listID")
	if !ok {
		return
	}
	itemID, ok := pathUUID(w, r, "itemID")
	if !ok {
		return
	}
	var p items.Patch
	if !decode(w, r, &p) {
		return
	}
	item, err := a.items.Patch(r.Context(), userID(r.Context()), listID, itemID, p)
	if err != nil {
		a.itemErr(w, "patch item", err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) deleteItem(w http.ResponseWriter, r *http.Request) {
	listID, ok := pathUUID(w, r, "listID")
	if !ok {
		return
	}
	itemID, ok := pathUUID(w, r, "itemID")
	if !ok {
		return
	}
	if err := a.items.Delete(r.Context(), userID(r.Context()), listID, itemID); err != nil {
		a.itemErr(w, "delete item", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) clearBought(w http.ResponseWriter, r *http.Request) {
	listID, ok := pathUUID(w, r, "listID")
	if !ok {
		return
	}
	n, err := a.items.ClearBought(r.Context(), userID(r.Context()), listID)
	if err != nil {
		a.itemErr(w, "clear bought", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"cleared": n})
}
