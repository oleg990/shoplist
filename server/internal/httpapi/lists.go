package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"shoplist/server/internal/lists"
)

func (a *API) listErr(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, lists.ErrNotFound):
		writeError(w, http.StatusNotFound, "list not found")
	case errors.Is(err, lists.ErrForbidden):
		writeError(w, http.StatusForbidden, "only the list owner can do this")
	case errors.Is(err, lists.ErrInvalidTitle):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, lists.ErrInviteInvalid):
		writeError(w, http.StatusNotFound, "invite is invalid, expired or used up")
	case errors.Is(err, lists.ErrListFull):
		writeError(w, http.StatusConflict, "list has too many members")
	case errors.Is(err, lists.ErrOwnerCannotLeave):
		writeError(w, http.StatusConflict, "the owner cannot leave the list; delete it instead")
	default:
		a.log.Error(op, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return uuid.Nil, false
	}
	return id, true
}

func (a *API) createList(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
	}
	if !decode(w, r, &in) {
		return
	}
	l, err := a.lists.Create(r.Context(), userID(r.Context()), in.Title)
	if err != nil {
		a.listErr(w, "create list", err)
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

func (a *API) listLists(w http.ResponseWriter, r *http.Request) {
	ls, err := a.lists.ListForUser(r.Context(), userID(r.Context()))
	if err != nil {
		a.listErr(w, "list lists", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": ls})
}

func (a *API) getList(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "listID")
	if !ok {
		return
	}
	l, err := a.lists.Get(r.Context(), userID(r.Context()), id)
	if err != nil {
		a.listErr(w, "get list", err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (a *API) renameList(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "listID")
	if !ok {
		return
	}
	var in struct {
		Title string `json:"title"`
	}
	if !decode(w, r, &in) {
		return
	}
	l, err := a.lists.Rename(r.Context(), userID(r.Context()), id, in.Title)
	if err != nil {
		a.listErr(w, "rename list", err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}

func (a *API) deleteList(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "listID")
	if !ok {
		return
	}
	if err := a.lists.Delete(r.Context(), userID(r.Context()), id); err != nil {
		a.listErr(w, "delete list", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listMembers(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "listID")
	if !ok {
		return
	}
	ms, err := a.lists.Members(r.Context(), userID(r.Context()), id)
	if err != nil {
		a.listErr(w, "list members", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": ms})
}

func (a *API) removeMember(w http.ResponseWriter, r *http.Request) {
	listID, ok := pathUUID(w, r, "listID")
	if !ok {
		return
	}
	targetID, ok := pathUUID(w, r, "userID")
	if !ok {
		return
	}
	if err := a.lists.RemoveMember(r.Context(), userID(r.Context()), listID, targetID); err != nil {
		a.listErr(w, "remove member", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) createInvite(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "listID")
	if !ok {
		return
	}
	// Тело необязательно: без него действуют значения по умолчанию.
	var in struct {
		TTLHours int `json:"ttl_hours"`
		MaxUses  int `json:"max_uses"`
	}
	if r.ContentLength != 0 && !decode(w, r, &in) {
		return
	}
	inv, err := a.lists.CreateInvite(r.Context(), userID(r.Context()), id, time.Duration(in.TTLHours)*time.Hour, in.MaxUses)
	if err != nil {
		a.listErr(w, "create invite", err)
		return
	}
	writeJSON(w, http.StatusCreated, inv)
}

func (a *API) acceptInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	l, err := a.lists.AcceptInvite(r.Context(), userID(r.Context()), in.Code)
	if err != nil {
		a.listErr(w, "accept invite", err)
		return
	}
	writeJSON(w, http.StatusOK, l)
}
