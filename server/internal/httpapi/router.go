// Package httpapi содержит HTTP-роутер и обработчики API.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"shoplist/server/internal/auth"
	"shoplist/server/internal/items"
	"shoplist/server/internal/lists"
	"shoplist/server/internal/realtime"
	"shoplist/server/internal/store"
)

// Pinger проверяет доступность базы данных.
type Pinger interface {
	Ping(ctx context.Context) error
}

type API struct {
	db      Pinger
	queries *store.Queries
	auth    *auth.Service
	lists   *lists.Service
	items   *items.Service
	hub     *realtime.Hub
	log     *slog.Logger
}

func NewRouter(db Pinger, queries *store.Queries, authSvc *auth.Service, listsSvc *lists.Service, itemsSvc *items.Service, hub *realtime.Hub, log *slog.Logger) http.Handler {
	a := &API{db: db, queries: queries, auth: authSvc, lists: listsSvc, items: itemsSvc, hub: hub, log: log}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	// WebSocket живёт долго, поэтому таймаут запроса применяется только к обычным маршрутам.
	r.Get("/api/v1/ws", a.ws)

	r.Group(func(r chi.Router) {
		r.Use(middleware.Timeout(15 * time.Second))

		r.Get("/healthz", a.health)
		r.Route("/api/v1", func(r chi.Router) {
			r.Get("/catalog/search", a.searchCatalog)
			r.Get("/categories", a.listCategories)

			r.Post("/auth/request-code", a.requestCode)
			r.Post("/auth/verify", a.verifyCode)
			r.Post("/auth/refresh", a.refresh)
			r.Post("/auth/logout", a.logout)

			r.Group(func(r chi.Router) {
				r.Use(a.requireAuth)
				r.Get("/me", a.me)
				r.Patch("/me", a.updateMe)
				r.Delete("/me", a.deleteMe)

				r.Post("/lists", a.createList)
				r.Get("/lists", a.listLists)
				r.Route("/lists/{listID}", func(r chi.Router) {
					r.Get("/", a.getList)
					r.Patch("/", a.renameList)
					r.Delete("/", a.deleteList)
					r.Get("/members", a.listMembers)
					r.Delete("/members/{userID}", a.removeMember)
					r.Post("/invites", a.createInvite)

					r.Get("/items", a.listItems)
					r.Post("/items/clear-bought", a.clearBought)
					r.Put("/items/{itemID}", a.putItem)
					r.Patch("/items/{itemID}", a.patchItem)
					r.Delete("/items/{itemID}", a.deleteItem)
				})
				r.Post("/invites/accept", a.acceptInvite)
			})
		})
	})
	return r
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	if err := a.db.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type catalogItem struct {
	ID           int32   `json:"id"`
	Name         string  `json:"name"`
	CategoryID   *int32  `json:"category_id"`
	CategoryName *string `json:"category_name"`
	DefaultUnit  *string `json:"default_unit"`
}

func (a *API) searchCatalog(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	limit := 20
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 50 {
			writeError(w, http.StatusBadRequest, "limit must be 1..50")
			return
		}
		limit = n
	}

	rows, err := a.queries.SearchCatalog(r.Context(), store.SearchCatalogParams{Query: q, MaxResults: int32(limit)})
	if err != nil {
		a.log.Error("search catalog", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	items := make([]catalogItem, 0, len(rows))
	for _, row := range rows {
		it := catalogItem{ID: row.ID, Name: row.Name}
		if row.CategoryID.Valid {
			it.CategoryID = &row.CategoryID.Int32
		}
		if row.CategoryName.Valid {
			it.CategoryName = &row.CategoryName.String
		}
		if row.DefaultUnit.Valid {
			it.DefaultUnit = &row.DefaultUnit.String
		}
		items = append(items, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) listCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := a.queries.ListCategories(r.Context())
	if err != nil {
		a.log.Error("list categories", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if cats == nil {
		cats = []store.Category{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": cats})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
