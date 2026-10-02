package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"shoplist/server/internal/auth"
)

type ctxKey struct{}

// userID возвращает id пользователя, положенный middleware requireAuth.
func userID(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(ctxKey{}).(uuid.UUID)
	return id
}

func (a *API) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		id, err := a.auth.ParseAccessToken(token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

// decode читает JSON-тело ограниченного размера.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func (a *API) requestCode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if !decode(w, r, &in) {
		return
	}
	switch err := a.auth.RequestCode(r.Context(), in.Email); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, auth.ErrInvalidEmail):
		writeError(w, http.StatusBadRequest, "invalid email")
	case errors.Is(err, auth.ErrTooManyCodes):
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "code was requested recently, try again in a minute")
	default:
		a.log.Error("request code", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func (a *API) verifyCode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	tokens, err := a.auth.VerifyCode(r.Context(), in.Email, in.Code)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, tokens)
	case errors.Is(err, auth.ErrInvalidEmail):
		writeError(w, http.StatusBadRequest, "invalid email")
	case errors.Is(err, auth.ErrInvalidCode):
		writeError(w, http.StatusUnauthorized, "invalid or expired code")
	default:
		a.log.Error("verify code", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func (a *API) refresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !decode(w, r, &in) {
		return
	}
	tokens, err := a.auth.Refresh(r.Context(), in.RefreshToken)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, tokens)
	case errors.Is(err, auth.ErrInvalidToken):
		writeError(w, http.StatusUnauthorized, "invalid refresh token")
	default:
		a.log.Error("refresh", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := a.auth.Logout(r.Context(), in.RefreshToken); err != nil {
		a.log.Error("logout", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	u, err := a.auth.GetUser(r.Context(), userID(r.Context()))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "user not found")
		return
	} else if err != nil {
		a.log.Error("get me", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (a *API) updateMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	if n := len([]rune(strings.TrimSpace(in.Name))); n == 0 || n > 50 {
		writeError(w, http.StatusBadRequest, "name must be 1..50 characters")
		return
	}
	u, err := a.auth.UpdateName(r.Context(), userID(r.Context()), in.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "user not found")
		return
	} else if err != nil {
		a.log.Error("update me", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (a *API) deleteMe(w http.ResponseWriter, r *http.Request) {
	if err := a.auth.DeleteAccount(r.Context(), userID(r.Context())); err != nil {
		a.log.Error("delete me", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
