package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
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

// authError переводит ошибки сервиса auth в HTTP-ответы; false, если ошибка не распознана.
func (a *API) authError(w http.ResponseWriter, op string, err error) {
	var locked auth.LockedError
	switch {
	case errors.As(err, &locked):
		secs := int(locked.RetryAfter.Seconds()) + 1
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeError(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
	case errors.Is(err, auth.ErrInvalidUsername), errors.Is(err, auth.ErrWeakPassword):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, auth.ErrUsernameTaken):
		writeError(w, http.StatusConflict, "username is taken")
	case errors.Is(err, auth.ErrInvalidLogin), errors.Is(err, auth.ErrInvalidRecovery):
		writeError(w, http.StatusUnauthorized, err.Error())
	default:
		a.log.Error(op, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if !decode(w, r, &in) {
		return
	}
	tokens, err := a.auth.Register(r.Context(), in.Username, in.Password, in.Name)
	if err != nil {
		a.authError(w, "register", err)
		return
	}
	writeJSON(w, http.StatusCreated, tokens)
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	tokens, err := a.auth.Login(r.Context(), in.Username, in.Password)
	if err != nil {
		a.authError(w, "login", err)
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

func (a *API) recoverAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username    string `json:"username"`
		Code        string `json:"code"`
		NewPassword string `json:"new_password"`
	}
	if !decode(w, r, &in) {
		return
	}
	tokens, err := a.auth.Recover(r.Context(), in.Username, in.Code, in.NewPassword)
	if err != nil {
		a.authError(w, "recover", err)
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if !decode(w, r, &in) {
		return
	}
	tokens, err := a.auth.ChangePassword(r.Context(), userID(r.Context()), in.OldPassword, in.NewPassword)
	if err != nil {
		a.authError(w, "change password", err)
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

func (a *API) recoveryCodesCount(w http.ResponseWriter, r *http.Request) {
	n, err := a.auth.UnusedRecoveryCodes(r.Context(), userID(r.Context()))
	if err != nil {
		a.authError(w, "recovery codes count", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"unused": n})
}

func (a *API) regenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	codes, err := a.auth.NewRecoveryCodes(r.Context(), userID(r.Context()), in.Password)
	if err != nil {
		a.authError(w, "regenerate recovery codes", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]string{"recovery_codes": codes})
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
