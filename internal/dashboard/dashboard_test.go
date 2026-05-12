package dashboard_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Ali-jj99/mcp-gateway/internal/auth"
	"github.com/Ali-jj99/mcp-gateway/internal/dashboard"
	"github.com/Ali-jj99/mcp-gateway/internal/store"
)

type mockQuerier struct {
	keys  []store.ListAPIKeysRow
	roles []store.Role
	perms map[uuid.UUID][]store.Permission
}

func newMock() *mockQuerier {
	return &mockQuerier{perms: make(map[uuid.UUID][]store.Permission)}
}

func (m *mockQuerier) CountActiveKeys(_ context.Context) (int64, error) {
	return int64(len(m.keys)), nil
}
func (m *mockQuerier) CountRequestsToday(_ context.Context) (int64, error) { return 42, nil }
func (m *mockQuerier) CountErrorsToday(_ context.Context) (int64, error)   { return 3, nil }
func (m *mockQuerier) ListAPIKeys(_ context.Context) ([]store.ListAPIKeysRow, error) {
	return m.keys, nil
}
func (m *mockQuerier) CreateAPIKey(_ context.Context, arg store.CreateAPIKeyParams) (store.ApiKey, error) {
	return store.ApiKey{ID: uuid.New(), Name: arg.Name, KeyHash: arg.KeyHash, KeyPrefix: arg.KeyPrefix, ExpiresAt: arg.ExpiresAt, Active: true}, nil
}
func (m *mockQuerier) RevokeAPIKey(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockQuerier) DeleteAPIKey(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockQuerier) GetAPIKeyByHash(_ context.Context, _ string) (store.ApiKey, error) {
	return store.ApiKey{}, sql.ErrNoRows
}
func (m *mockQuerier) ListRoles(_ context.Context) ([]store.Role, error) { return m.roles, nil }
func (m *mockQuerier) CreateRole(_ context.Context, arg store.CreateRoleParams) (store.Role, error) {
	return store.Role{ID: uuid.New(), Name: arg.Name, Description: arg.Description}, nil
}
func (m *mockQuerier) DeleteRole(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockQuerier) GetRoleByName(_ context.Context, _ string) (store.Role, error) {
	return store.Role{}, sql.ErrNoRows
}
func (m *mockQuerier) ListPermissionsByRole(_ context.Context, roleID uuid.UUID) ([]store.Permission, error) {
	return m.perms[roleID], nil
}
func (m *mockQuerier) AddPermission(_ context.Context, arg store.AddPermissionParams) (store.Permission, error) {
	return store.Permission{ID: uuid.New(), RoleID: arg.RoleID, Resource: arg.Resource, Action: arg.Action}, nil
}
func (m *mockQuerier) DeletePermission(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockQuerier) AssignRoleToKey(_ context.Context, _ store.AssignRoleToKeyParams) error {
	return nil
}
func (m *mockQuerier) RemoveRoleFromKey(_ context.Context, _ store.RemoveRoleFromKeyParams) error {
	return nil
}
func (m *mockQuerier) ListRolesForKey(_ context.Context, _ uuid.UUID) ([]store.Role, error) {
	return nil, nil
}
func (m *mockQuerier) GetPermissionsByKeyID(_ context.Context, _ uuid.UUID) ([]store.GetPermissionsByKeyIDRow, error) {
	return nil, nil
}
func (m *mockQuerier) InsertAuditLog(_ context.Context, _ store.InsertAuditLogParams) error {
	return nil
}
func (m *mockQuerier) ListAuditLogs(_ context.Context, _ store.ListAuditLogsParams) ([]store.ListAuditLogsRow, error) {
	return nil, nil
}
func (m *mockQuerier) GetRateLimitByKeyID(_ context.Context, _ uuid.UUID) (store.GetRateLimitByKeyIDRow, error) {
	return store.GetRateLimitByKeyIDRow{}, sql.ErrNoRows
}
func (m *mockQuerier) UpsertRateLimit(_ context.Context, _ store.UpsertRateLimitParams) (store.UpsertRateLimitRow, error) {
	return store.UpsertRateLimitRow{}, nil
}
func (m *mockQuerier) CreatePolicy(_ context.Context, _ store.CreatePolicyParams) (store.Policy, error) {
	return store.Policy{}, nil
}
func (m *mockQuerier) GetPolicy(_ context.Context, _ uuid.UUID) (store.Policy, error) {
	return store.Policy{}, nil
}
func (m *mockQuerier) ListPolicies(_ context.Context) ([]store.Policy, error)        { return nil, nil }
func (m *mockQuerier) ListEnabledPolicies(_ context.Context) ([]store.Policy, error) { return nil, nil }
func (m *mockQuerier) UpdatePolicy(_ context.Context, _ store.UpdatePolicyParams) (store.Policy, error) {
	return store.Policy{}, nil
}
func (m *mockQuerier) DeletePolicy(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockQuerier) TogglePolicy(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockQuerier) CountRequestsByHour(_ context.Context) ([]store.CountRequestsByHourRow, error) {
	return nil, nil
}

func newTestServer() (*dashboard.Server, *mockQuerier) {
	mock := newMock()
	authSvc := auth.NewService(mock)
	secret := []byte("test-secret-key-at-least-32bytes!")
	srv := dashboard.NewServer(mock, authSvc, secret, "admin", "password123")
	return srv, mock
}

func login(t *testing.T, srv *dashboard.Server) []*http.Cookie {
	t.Helper()
	form := url.Values{"username": {"admin"}, "password": {"password123"}}
	req := httptest.NewRequest("POST", "/dashboard/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login expected 303, got %d", w.Code)
	}
	return w.Result().Cookies()
}

// --- Auth tests ---

func TestLoginPage_Renders(t *testing.T) {
	srv, _ := newTestServer()
	req := httptest.NewRequest("GET", "/dashboard/login", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Sign In") {
		t.Fatal("login page should contain Sign In button")
	}
}

func TestLogin_ValidCredentials(t *testing.T) {
	srv, _ := newTestServer()
	cookies := login(t, srv)

	found := false
	for _, c := range cookies {
		if c.Name == "mcp_session" && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected mcp_session cookie after login")
	}
}

func TestLogin_InvalidCredentials(t *testing.T) {
	srv, _ := newTestServer()
	form := url.Values{"username": {"admin"}, "password": {"wrong"}}
	req := httptest.NewRequest("POST", "/dashboard/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (re-render login), got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Invalid username or password") {
		t.Fatal("should show error message")
	}
}

func TestProtectedRoute_RedirectsWithoutAuth(t *testing.T) {
	srv, _ := newTestServer()
	req := httptest.NewRequest("GET", "/dashboard", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect, got %d", w.Code)
	}
	if w.Header().Get("Location") != "/dashboard/login" {
		t.Fatalf("expected redirect to /dashboard/login, got %q", w.Header().Get("Location"))
	}
}

func TestLogout_ClearsCookie(t *testing.T) {
	srv, _ := newTestServer()
	cookies := login(t, srv)

	req := httptest.NewRequest("GET", "/dashboard/logout", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("expected 303, got %d", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "mcp_session" && c.MaxAge < 0 {
			return
		}
	}
	t.Fatal("expected mcp_session cookie to be cleared")
}

// --- Dashboard page tests ---

func TestDashboard_ShowsStats(t *testing.T) {
	srv, _ := newTestServer()
	cookies := login(t, srv)

	req := httptest.NewRequest("GET", "/dashboard", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "42") {
		t.Fatal("should contain request count")
	}
	if !strings.Contains(body, "Requests Today") {
		t.Fatal("should contain stats labels")
	}
}

func TestStatsEndpoint_ReturnsFragment(t *testing.T) {
	srv, _ := newTestServer()
	cookies := login(t, srv)

	req := httptest.NewRequest("GET", "/dashboard/stats", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "42") {
		t.Fatal("stats fragment should contain request count")
	}
	if strings.Contains(body, "<!DOCTYPE html>") {
		t.Fatal("stats fragment should not be a full page")
	}
}

// --- Keys page tests ---

func TestKeysPage_Renders(t *testing.T) {
	srv, _ := newTestServer()
	cookies := login(t, srv)

	req := httptest.NewRequest("GET", "/dashboard/keys", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "API Keys") {
		t.Fatal("should contain API Keys heading")
	}
}

func TestCreateKey_ReturnsTable(t *testing.T) {
	srv, _ := newTestServer()
	cookies := login(t, srv)

	form := url.Values{"name": {"test-key"}, "expires": {"0"}}
	req := httptest.NewRequest("POST", "/dashboard/keys", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "mcpgw_") {
		t.Fatal("response should contain the new plaintext key")
	}
}

// --- Audit page tests ---

func TestAuditPage_Renders(t *testing.T) {
	srv, _ := newTestServer()
	cookies := login(t, srv)

	req := httptest.NewRequest("GET", "/dashboard/audit", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Audit Logs") {
		t.Fatal("should contain Audit Logs heading")
	}
}

func TestAuditResults_ReturnsFragment(t *testing.T) {
	srv, _ := newTestServer()
	cookies := login(t, srv)

	req := httptest.NewRequest("GET", "/dashboard/audit/results?tool_name=test&status_code=200", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "<!DOCTYPE html>") {
		t.Fatal("audit results fragment should not be a full page")
	}
}

// --- Roles page tests ---

func TestRolesPage_Renders(t *testing.T) {
	srv, _ := newTestServer()
	cookies := login(t, srv)

	req := httptest.NewRequest("GET", "/dashboard/roles", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Roles") {
		t.Fatal("should contain Roles heading")
	}
}

func TestCreateRole_ReturnsUpdatedList(t *testing.T) {
	srv, _ := newTestServer()
	cookies := login(t, srv)

	form := url.Values{"name": {"reader"}, "description": {"Read-only access"}}
	req := httptest.NewRequest("POST", "/dashboard/roles", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
