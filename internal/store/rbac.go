package store

import (
	"context"
	"sort"
	"time"
)

// AdminUser represents an admin user in the gateway.
type AdminUser struct {
	ID        string     `json:"id"`
	Subject   string     `json:"subject,omitempty"`
	Email     string     `json:"email,omitempty"`
	Role      string     `json:"role"`
	CreatedAt time.Time  `json:"created_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// Valid roles.
const (
	RoleAdmin  = "admin"
	RoleEditor = "editor"
	RoleViewer = "viewer"
)

// validRoles is the set of accepted role strings.
var validRoles = map[string]bool{
	RoleAdmin:  true,
	RoleEditor: true,
	RoleViewer: true,
}

// rolePermissions maps each role to its allowed actions.
var rolePermissions = map[string]map[string]bool{
	RoleAdmin: {
		"project:create": true,
		"project:delete": true,
		"project:list":   true,
		"key:create":     true,
		"key:revoke":     true,
		"key:list":       true,
		"usage:read":     true,
		"audit:read":     true,
		"user:manage":    true,
	},
	RoleEditor: {
		"project:list": true,
		"key:create":   true,
		"key:revoke":   true,
		"key:list":     true,
		"usage:read":   true,
		"audit:read":   true,
	},
	RoleViewer: {
		"project:list": true,
		"key:list":     true,
		"usage:read":   true,
		"audit:read":   true,
	},
}

// GetRolePermissions returns a copy of the role-permission matrix.
// The returned map is safe to read without synchronization.
func GetRolePermissions() map[string][]string {
	result := make(map[string][]string, len(rolePermissions))
	for role, perms := range rolePermissions {
		actions := make([]string, 0, len(perms))
		for action := range perms {
			actions = append(actions, action)
		}
		sort.Strings(actions)
		result[role] = actions
	}
	return result
}

// RoleAllowed checks if a role has permission for the given action.
// Unknown roles return false for all actions.
func RoleAllowed(role, action string) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	return perms[action]
}

// ValidateRole returns true if the role string is recognized.
func ValidateRole(role string) bool {
	return validRoles[role]
}

// CreateUser inserts a new admin user and returns it with the generated ID.
func CreateUser(ctx context.Context, db Queryer, subject, email, role string) (*AdminUser, error) {
	u := &AdminUser{Subject: subject, Email: email, Role: role}
	err := db.QueryRowContext(ctx,
		`INSERT INTO admin_users (subject, email, role)
		 VALUES ($1, $2, $3)
		 RETURNING id, created_at`,
		nullIfEmpty(subject), nullIfEmpty(email), role,
	).Scan(&u.ID, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// LookupUserBySubject finds a non-deleted user by OIDC subject claim.
func LookupUserBySubject(ctx context.Context, db Queryer, subject string) (*AdminUser, error) {
	u := &AdminUser{}
	err := db.QueryRowContext(ctx,
		`SELECT id, COALESCE(subject,''), COALESCE(email,''), role, created_at
		 FROM admin_users
		 WHERE subject = $1 AND deleted_at IS NULL`,
		subject,
	).Scan(&u.ID, &u.Subject, &u.Email, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// LookupUserByEmail finds a non-deleted user by email.
func LookupUserByEmail(ctx context.Context, db Queryer, email string) (*AdminUser, error) {
	u := &AdminUser{}
	err := db.QueryRowContext(ctx,
		`SELECT id, COALESCE(subject,''), COALESCE(email,''), role, created_at
		 FROM admin_users
		 WHERE email = $1 AND deleted_at IS NULL`,
		email,
	).Scan(&u.ID, &u.Subject, &u.Email, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// LookupUserByID finds a non-deleted user by ID.
func LookupUserByID(ctx context.Context, db Queryer, id string) (*AdminUser, error) {
	u := &AdminUser{}
	err := db.QueryRowContext(ctx,
		`SELECT id, COALESCE(subject,''), COALESCE(email,''), role, created_at
		 FROM admin_users
		 WHERE id = $1 AND deleted_at IS NULL`,
		id,
	).Scan(&u.ID, &u.Subject, &u.Email, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// ListUsers returns all non-deleted admin users ordered by created_at.
func ListUsers(ctx context.Context, db Queryer) ([]AdminUser, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, COALESCE(subject,''), COALESCE(email,''), role, created_at
		 FROM admin_users
		 WHERE deleted_at IS NULL
		 ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []AdminUser
	for rows.Next() {
		var u AdminUser
		if err := rows.Scan(&u.ID, &u.Subject, &u.Email, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// UpdateUserRole changes a user's role.
func UpdateUserRole(ctx context.Context, db Queryer, id, role string) error {
	res, err := db.ExecContext(ctx,
		`UPDATE admin_users SET role = $1 WHERE id = $2 AND deleted_at IS NULL`,
		role, id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SoftDeleteUser sets deleted_at on a user (preserves audit trail).
func SoftDeleteUser(ctx context.Context, db Queryer, id string) error {
	res, err := db.ExecContext(ctx,
		`UPDATE admin_users SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// nullIfEmpty returns nil for empty strings (for nullable DB columns).
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
