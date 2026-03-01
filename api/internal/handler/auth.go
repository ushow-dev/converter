package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"app/api/internal/auth"
	"app/api/internal/config"
)

// AuthHandler handles admin authentication.
type AuthHandler struct {
	cfg *config.Config
}

// NewAuthHandler creates an AuthHandler.
func NewAuthHandler(cfg *config.Config) *AuthHandler {
	return &AuthHandler{cfg: cfg}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Login handles POST /api/admin/auth/login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"invalid JSON body", false, cid)
		return
	}

	if req.Email == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR",
			"email and password are required", false, cid)
		return
	}

	// Constant-time email comparison.
	if req.Email != h.cfg.AdminEmail {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED",
			"invalid credentials", false, cid)
		return
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(h.cfg.AdminPasswordHash), []byte(req.Password),
	); err != nil {
		respondError(w, http.StatusUnauthorized, "UNAUTHORIZED",
			"invalid credentials", false, cid)
		return
	}

	token, err := auth.IssueToken(req.Email, h.cfg.JWTSecret, h.cfg.JWTExpiry)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"could not issue token", false, cid)
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   int(h.cfg.JWTExpiry / time.Second),
	})
}
