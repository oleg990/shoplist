package httpapi

import (
	"errors"
	"net/http"

	"shoplist/server/internal/push"
)

// registerDevice сохраняет Expo push-токен устройства. Приложение вызывает его после входа и при смене токена.
func (a *API) registerDevice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ExpoPushToken string `json:"expo_push_token"`
		Platform      string `json:"platform"`
	}
	if !decode(w, r, &in) {
		return
	}
	switch err := a.push.RegisterDevice(r.Context(), userID(r.Context()), in.ExpoPushToken, in.Platform); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, push.ErrInvalidToken), errors.Is(err, push.ErrInvalidPlatform):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		a.log.Error("register device", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// unregisterDevice удаляет токен; приложение вызывает его перед выходом из аккаунта.
func (a *API) unregisterDevice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ExpoPushToken string `json:"expo_push_token"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := a.push.UnregisterDevice(r.Context(), userID(r.Context()), in.ExpoPushToken); err != nil {
		a.log.Error("unregister device", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
