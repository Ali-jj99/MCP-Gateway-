package dashboard

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Ali-jj99/mcp-gateway/internal/auth"
	"github.com/Ali-jj99/mcp-gateway/internal/store"
)

type Server struct {
	q         store.Querier
	authSvc   *auth.Service
	jwtSecret []byte
	adminUser string
	adminPass string
	router    chi.Router
}

func NewServer(q store.Querier, authSvc *auth.Service, jwtSecret []byte, adminUser, adminPass string) *Server {
	s := &Server{
		q:         q,
		authSvc:   authSvc,
		jwtSecret: jwtSecret,
		adminUser: adminUser,
		adminPass: adminPass,
		router:    chi.NewRouter(),
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.router.Get("/dashboard/login", s.handleLoginPage)
	s.router.Post("/dashboard/login", s.handleLogin)
	s.router.Get("/dashboard/logout", s.handleLogout)

	s.router.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/dashboard", s.handleHome)
		r.Get("/dashboard/stats", s.handleStats)
		r.Get("/dashboard/chart-data", s.handleChartData)

		r.Get("/dashboard/keys", s.handleKeys)
		r.Post("/dashboard/keys", s.handleCreateKey)
		r.Post("/dashboard/keys/{id}/revoke", s.handleRevokeKey)
		r.Delete("/dashboard/keys/{id}", s.handleDeleteKey)

		r.Get("/dashboard/audit", s.handleAudit)
		r.Get("/dashboard/audit/results", s.handleAuditResults)

		r.Get("/dashboard/roles", s.handleRoles)
		r.Post("/dashboard/roles", s.handleCreateRole)
		r.Delete("/dashboard/roles/{id}", s.handleDeleteRole)
		r.Post("/dashboard/roles/{roleID}/permissions", s.handleAddPermission)
		r.Delete("/dashboard/permissions/{id}", s.handleDeletePermission)

		r.Get("/dashboard/policies", s.handlePolicies)
		r.Post("/dashboard/policies", s.handleCreatePolicy)
		r.Post("/dashboard/policies/{id}/toggle", s.handleTogglePolicy)
		r.Delete("/dashboard/policies/{id}", s.handleDeletePolicy)
	})
}

func render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		slog.Error("template render error", "error", err)
	}
}

// --- Auth handlers ---

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		if _, ok := validateToken(s.jwtSecret, cookie.Value); ok {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
	}
	render(w, r, LoginPage(""))
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte(s.adminUser))
	passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(s.adminPass))
	if usernameMatch&passwordMatch != 1 {
		render(w, r, LoginPage("Invalid username or password"))
		return
	}

	if err := setSessionCookie(w, s.jwtSecret, username); err != nil {
		render(w, r, LoginPage("Internal error"))
		return
	}
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w)
	http.Redirect(w, r, "/dashboard/login", http.StatusSeeOther)
}

// --- Dashboard home ---

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	render(w, r, HomePage(s.loadStats(r)))
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	render(w, r, StatsCards(s.loadStats(r)))
}

func (s *Server) loadStats(r *http.Request) Stats {
	ctx := r.Context()
	reqToday, _ := s.q.CountRequestsToday(ctx)
	activeKeys, _ := s.q.CountActiveKeys(ctx)
	errToday, _ := s.q.CountErrorsToday(ctx)

	rate := "0%"
	if reqToday > 0 {
		rate = fmt.Sprintf("%.1f%%", float64(errToday)/float64(reqToday)*100)
	}
	return Stats{
		RequestsToday: reqToday,
		ActiveKeys:    activeKeys,
		ErrorsToday:   errToday,
		ErrorRate:     rate,
	}
}

// --- Keys handlers ---

func (s *Server) handleKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.q.ListAPIKeys(r.Context())
	if err != nil {
		slog.Error("list keys failed", "error", err)
		keys = nil
	}
	render(w, r, KeysPage(keys))
}

func (s *Server) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	expiresStr := r.FormValue("expires")

	var expiresAt *time.Time
	if expiresStr != "" && expiresStr != "0" {
		dur, err := time.ParseDuration(expiresStr)
		if err == nil {
			t := time.Now().Add(dur)
			expiresAt = &t
		}
	}

	plaintext, _, err := s.authSvc.CreateKey(r.Context(), name, expiresAt)
	if err != nil {
		slog.Error("create key failed", "error", err)
	}

	keys, _ := s.q.ListAPIKeys(r.Context())

	// Respond with updated table + key banner via HTMX OOB
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = KeysTable(keys).Render(r.Context(), w)
	if plaintext != "" {
		_, _ = w.Write([]byte(`<div id="new-key-display" hx-swap-oob="innerHTML">`))
		_ = NewKeyBanner(plaintext).Render(r.Context(), w)
		_, _ = w.Write([]byte(`</div>`))
	}
}

func (s *Server) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	_ = s.q.RevokeAPIKey(r.Context(), id)
	keys, _ := s.q.ListAPIKeys(r.Context())
	render(w, r, KeysTable(keys))
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	_ = s.q.DeleteAPIKey(r.Context(), id)
	keys, _ := s.q.ListAPIKeys(r.Context())
	render(w, r, KeysTable(keys))
}

// --- Audit handlers ---

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	logs, f := s.queryAuditLogs(r)
	render(w, r, AuditPage(logs, f))
}

func (s *Server) handleAuditResults(w http.ResponseWriter, r *http.Request) {
	logs, f := s.queryAuditLogs(r)
	render(w, r, AuditResults(logs, f))
}

const pageSize = 50

func (s *Server) queryAuditLogs(r *http.Request) ([]store.ListAuditLogsRow, AuditFilter) {
	f := AuditFilter{
		ToolName:   r.URL.Query().Get("tool_name"),
		StatusCode: r.URL.Query().Get("status_code"),
		Page:       1,
	}
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		f.Page = p
	}

	params := store.ListAuditLogsParams{
		PageLimit: int32(pageSize + 1),
	}
	if f.ToolName != "" {
		params.ToolName = sql.NullString{String: f.ToolName, Valid: true}
	}
	if f.StatusCode != "" {
		if code, err := strconv.Atoi(f.StatusCode); err == nil {
			params.StatusCode = sql.NullInt32{Int32: int32(code), Valid: true}
		}
	}

	logs, err := s.q.ListAuditLogs(r.Context(), params)
	if err != nil {
		slog.Error("list audit logs failed", "error", err)
		return nil, f
	}

	if len(logs) > pageSize {
		f.HasMore = true
		logs = logs[:pageSize]
	}
	return logs, f
}

// --- Roles handlers ---

func (s *Server) handleRoles(w http.ResponseWriter, r *http.Request) {
	render(w, r, RolesPage(s.loadRolesWithPerms(r)))
}

func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	desc := r.FormValue("description")
	_, err := s.q.CreateRole(r.Context(), store.CreateRoleParams{Name: name, Description: desc})
	if err != nil {
		slog.Error("create role failed", "error", err)
	}
	render(w, r, RolesList(s.loadRolesWithPerms(r)))
}

func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	_ = s.q.DeleteRole(r.Context(), id)
	render(w, r, RolesList(s.loadRolesWithPerms(r)))
}

func (s *Server) handleAddPermission(w http.ResponseWriter, r *http.Request) {
	roleID, err := uuid.Parse(chi.URLParam(r, "roleID"))
	if err != nil {
		http.Error(w, "invalid role id", http.StatusBadRequest)
		return
	}
	resource := r.FormValue("resource")
	action := r.FormValue("action")
	_, err = s.q.AddPermission(r.Context(), store.AddPermissionParams{
		RoleID:   roleID,
		Resource: resource,
		Action:   action,
	})
	if err != nil {
		slog.Error("add permission failed", "error", err)
	}
	render(w, r, RolesList(s.loadRolesWithPerms(r)))
}

func (s *Server) handleDeletePermission(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	_ = s.q.DeletePermission(r.Context(), id)
	render(w, r, RolesList(s.loadRolesWithPerms(r)))
}

func (s *Server) loadRolesWithPerms(r *http.Request) []RoleWithPerms {
	roles, err := s.q.ListRoles(r.Context())
	if err != nil {
		slog.Error("list roles failed", "error", err)
		return nil
	}
	result := make([]RoleWithPerms, len(roles))
	for i, role := range roles {
		perms, _ := s.q.ListPermissionsByRole(r.Context(), role.ID)
		result[i] = RoleWithPerms{Role: role, Permissions: perms}
	}
	return result
}

// --- Policy handlers ---

func (s *Server) handlePolicies(w http.ResponseWriter, r *http.Request) {
	policies, err := s.q.ListPolicies(r.Context())
	if err != nil {
		slog.Error("list policies failed", "error", err)
		policies = nil
	}
	render(w, r, PoliciesPage(policies))
}

func (s *Server) handleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	policyType := r.FormValue("policy_type")
	configStr := r.FormValue("config")

	var cfg json.RawMessage
	if err := json.Unmarshal([]byte(configStr), &cfg); err != nil {
		slog.Error("invalid policy config JSON", "error", err)
		policies, _ := s.q.ListPolicies(r.Context())
		render(w, r, PoliciesList(policies))
		return
	}

	_, err := s.q.CreatePolicy(r.Context(), store.CreatePolicyParams{
		Name:       name,
		PolicyType: policyType,
		Enabled:    true,
		Config:     cfg,
	})
	if err != nil {
		slog.Error("create policy failed", "error", err)
	}

	policies, _ := s.q.ListPolicies(r.Context())
	render(w, r, PoliciesList(policies))
}

func (s *Server) handleTogglePolicy(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	_ = s.q.TogglePolicy(r.Context(), id)
	policies, _ := s.q.ListPolicies(r.Context())
	render(w, r, PoliciesList(policies))
}

func (s *Server) handleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	_ = s.q.DeletePolicy(r.Context(), id)
	policies, _ := s.q.ListPolicies(r.Context())
	render(w, r, PoliciesList(policies))
}

// --- Chart data ---

func (s *Server) handleChartData(w http.ResponseWriter, r *http.Request) {
	rows, _ := s.q.CountRequestsByHour(r.Context())

	now := time.Now().UTC().Truncate(time.Hour)
	labels := make([]string, 24)
	data := make([]int64, 24)

	lookup := make(map[int64]int64, len(rows))
	for _, row := range rows {
		lookup[row.HourEpoch] = row.Count
	}

	for i := range 24 {
		t := now.Add(-time.Duration(23-i) * time.Hour)
		labels[i] = t.Format("15:04")
		data[i] = lookup[t.Unix()]
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"labels": labels,
		"data":   data,
	})
}
