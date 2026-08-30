package auth

import (
	"encoding/json"
	"net/http"

	openapi "openagentx/internal/api"
	web "openagentx/internal/auth/web"
)

type Handler struct {
	manager *web.Manager
	mux     *http.ServeMux
}

func NewHandler(manager *web.Manager) *Handler {
	h := &Handler{manager: manager, mux: http.NewServeMux()}
	h.mux.HandleFunc("POST "+openapi.AuthLoginPath, h.login)
	h.mux.HandleFunc("POST "+openapi.AuthLogoutPath, h.logout)
	h.mux.HandleFunc("GET "+openapi.AuthSessionPath, h.session)
	return h
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	h.mux.ServeHTTP(w, r)
}
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req openapi.LoginRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil || req.Validate() != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	s, err := h.manager.Login(req.Username, req.Password)
	if err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	web.SetSessionCookie(w, s)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessionResponse(s))
}
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	s, err := h.manager.Authenticate(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if web.ValidateCSRF(s, r.Header.Get("X-CSRF-Token")) != nil {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	if err := h.manager.Revoke(r.Context(), s); err != nil {
		http.Error(w, "logout failed", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "openagentx_session", Value: "", Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) session(w http.ResponseWriter, r *http.Request) {
	s, err := h.manager.Authenticate(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.manager.RefreshCSRF(r.Context(), s); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sessionResponse(s))
}

func sessionResponse(s *web.Session) openapi.WebSessionResponse {
	roles := make([]string, 0, len(s.User.Roles))
	for _, role := range s.User.Roles {
		roles = append(roles, string(role))
	}
	return openapi.WebSessionResponse{Principal: openapi.WebPrincipal{UserID: s.User.ID, Username: s.User.Username, Roles: roles}, CSRFToken: s.CSRFToken, IdleExpiresAt: s.IdleExpiresAt, AbsoluteExpiresAt: s.AbsoluteExpiresAt}
}
