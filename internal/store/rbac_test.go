package store

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestRoleAllowed(t *testing.T) {
	tests := []struct {
		role   string
		action string
		want   bool
	}{
		// Admin — everything allowed
		{RoleAdmin, "project:create", true},
		{RoleAdmin, "project:delete", true},
		{RoleAdmin, "project:list", true},
		{RoleAdmin, "key:create", true},
		{RoleAdmin, "key:revoke", true},
		{RoleAdmin, "key:list", true},
		{RoleAdmin, "usage:read", true},
		{RoleAdmin, "audit:read", true},
		{RoleAdmin, "user:manage", true},

		// Editor — no project create/delete, no user management
		{RoleEditor, "project:create", false},
		{RoleEditor, "project:delete", false},
		{RoleEditor, "project:list", true},
		{RoleEditor, "key:create", true},
		{RoleEditor, "key:revoke", true},
		{RoleEditor, "key:list", true},
		{RoleEditor, "usage:read", true},
		{RoleEditor, "audit:read", true},
		{RoleEditor, "user:manage", false},

		// Viewer — read-only
		{RoleViewer, "project:create", false},
		{RoleViewer, "project:delete", false},
		{RoleViewer, "project:list", true},
		{RoleViewer, "key:create", false},
		{RoleViewer, "key:revoke", false},
		{RoleViewer, "key:list", true},
		{RoleViewer, "usage:read", true},
		{RoleViewer, "audit:read", true},
		{RoleViewer, "user:manage", false},

		// Unknown role — nothing allowed (prevents privilege escalation)
		{"hacker", "project:create", false},
		{"hacker", "project:delete", false},
		{"hacker", "project:list", false},
		{"hacker", "key:create", false},
		{"hacker", "key:revoke", false},
		{"hacker", "key:list", false},
		{"hacker", "usage:read", false},
		{"hacker", "audit:read", false},
		{"hacker", "user:manage", false},
		{"", "project:list", false},

		// Unknown action
		{RoleAdmin, "unknown:action", false},
	}

	for _, tt := range tests {
		got := RoleAllowed(tt.role, tt.action)
		if got != tt.want {
			t.Errorf("RoleAllowed(%q, %q) = %v, want %v", tt.role, tt.action, got, tt.want)
		}
	}
}

func TestValidateRole(t *testing.T) {
	if !ValidateRole(RoleAdmin) {
		t.Error("ValidateRole(admin) = false, want true")
	}
	if !ValidateRole(RoleEditor) {
		t.Error("ValidateRole(editor) = false, want true")
	}
	if !ValidateRole(RoleViewer) {
		t.Error("ValidateRole(viewer) = false, want true")
	}
	if ValidateRole("superuser") {
		t.Error("ValidateRole(superuser) = true, want false")
	}
	if ValidateRole("") {
		t.Error("ValidateRole('') = true, want false")
	}
}

func TestGetRolePermissions(t *testing.T) {
	perms := GetRolePermissions()
	if len(perms) != 3 {
		t.Fatalf("expected 3 roles, got %d", len(perms))
	}
	// Verify admin has all permissions
	adminPerms := perms[RoleAdmin]
	if len(adminPerms) != 9 {
		t.Errorf("admin has %d permissions, want 9", len(adminPerms))
	}
	// Verify sorted
	for role, actions := range perms {
		for i := 1; i < len(actions); i++ {
			if actions[i] < actions[i-1] {
				t.Errorf("%s permissions not sorted: %v", role, actions)
			}
		}
	}
	// Verify consistency with RoleAllowed
	for role, actions := range perms {
		for _, action := range actions {
			if !RoleAllowed(role, action) {
				t.Errorf("GetRolePermissions says %s can %s, but RoleAllowed says no", role, action)
			}
		}
	}
}

func TestCreateUser(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ctx := context.Background()
	cleanAdminUsers(t, db)

	u, err := CreateUser(ctx, db, "oidc-12345", "alice@example.com", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.ID == "" {
		t.Error("expected non-empty ID")
	}
	if u.Subject != "oidc-12345" {
		t.Errorf("subject = %q, want %q", u.Subject, "oidc-12345")
	}
	if u.Email != "alice@example.com" {
		t.Errorf("email = %q, want %q", u.Email, "alice@example.com")
	}
	if u.Role != RoleAdmin {
		t.Errorf("role = %q, want %q", u.Role, RoleAdmin)
	}
}

func TestLookupUserBySubject(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ctx := context.Background()
	cleanAdminUsers(t, db)

	_, err := CreateUser(ctx, db, "sub-abc", "bob@example.com", RoleEditor)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	u, err := LookupUserBySubject(ctx, db, "sub-abc")
	if err != nil {
		t.Fatalf("LookupUserBySubject: %v", err)
	}
	if u.Email != "bob@example.com" {
		t.Errorf("email = %q, want %q", u.Email, "bob@example.com")
	}
	if u.Role != RoleEditor {
		t.Errorf("role = %q, want %q", u.Role, RoleEditor)
	}

	// Not found
	_, err = LookupUserBySubject(ctx, db, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent subject")
	}
}

func TestLookupUserByEmail(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ctx := context.Background()
	cleanAdminUsers(t, db)

	_, err := CreateUser(ctx, db, "sub-xyz", "carol@example.com", RoleViewer)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	u, err := LookupUserByEmail(ctx, db, "carol@example.com")
	if err != nil {
		t.Fatalf("LookupUserByEmail: %v", err)
	}
	if u.Subject != "sub-xyz" {
		t.Errorf("subject = %q, want %q", u.Subject, "sub-xyz")
	}

	// Not found
	_, err = LookupUserByEmail(ctx, db, "nobody@example.com")
	if err == nil {
		t.Error("expected error for nonexistent email")
	}
}

func TestUpdateUserRole(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ctx := context.Background()
	cleanAdminUsers(t, db)

	u, err := CreateUser(ctx, db, "sub-role", "role@example.com", RoleViewer)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	err = UpdateUserRole(ctx, db, u.ID, RoleAdmin)
	if err != nil {
		t.Fatalf("UpdateUserRole: %v", err)
	}

	updated, err := LookupUserByID(ctx, db, u.ID)
	if err != nil {
		t.Fatalf("LookupUserByID: %v", err)
	}
	if updated.Role != RoleAdmin {
		t.Errorf("role = %q, want %q", updated.Role, RoleAdmin)
	}

	// Not found
	err = UpdateUserRole(ctx, db, "nonexistent", RoleAdmin)
	if err != ErrNotFound {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestSoftDeleteUser(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ctx := context.Background()
	cleanAdminUsers(t, db)

	u, err := CreateUser(ctx, db, "sub-del", "delete@example.com", RoleViewer)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Verify user is visible
	_, err = LookupUserBySubject(ctx, db, "sub-del")
	if err != nil {
		t.Fatalf("LookupUserBySubject before delete: %v", err)
	}

	// Soft delete
	err = SoftDeleteUser(ctx, db, u.ID)
	if err != nil {
		t.Fatalf("SoftDeleteUser: %v", err)
	}

	// User should no longer be visible
	_, err = LookupUserBySubject(ctx, db, "sub-del")
	if err == nil {
		t.Error("expected error after soft delete, got nil")
	}

	// Double delete returns not found
	err = SoftDeleteUser(ctx, db, u.ID)
	if err != ErrNotFound {
		t.Errorf("double delete error = %v, want ErrNotFound", err)
	}
}

func TestCreateUserDuplicateSubject(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ctx := context.Background()
	cleanAdminUsers(t, db)

	_, err := CreateUser(ctx, db, "sub-dup", "first@example.com", RoleViewer)
	if err != nil {
		t.Fatalf("CreateUser first: %v", err)
	}

	_, err = CreateUser(ctx, db, "sub-dup", "second@example.com", RoleViewer)
	if err == nil {
		t.Error("expected error for duplicate subject, got nil")
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	url := testDBURL()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Skipf("cannot connect to postgres: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Skipf("cannot ping postgres: %v", err)
	}
	return db
}

func testDBURL() string {
	for _, env := range []string{"TEST_DATABASE_URL", "DATABASE_URL"} {
		if u := os.Getenv(env); u != "" {
			return u
		}
	}
	return "postgres://openlimit:openlimit@localhost:5432/openlimit_test?sslmode=disable"
}

func cleanAdminUsers(t *testing.T, db *sql.DB) {
	t.Helper()
	db.Exec("DELETE FROM admin_users")
}
