// Package identity owns users, authentication, and ward-scoped authorization.
package identity

import "time"

// Role mirrors the proto enum medisync.identity.v1.Role.
type Role string

const (
	RoleAdmin      Role = "ADMIN"
	RolePharmacist Role = "PHARMACIST"
	RoleNurse      Role = "NURSE"
	RoleRefiller   Role = "REFILLER"
)

// User is the domain model.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	DisplayName  string
	Role         Role
	WardIDs      []string
	Active       bool
	ProjectID    string    // empty = SYSADMIN (cross-project)
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// IsSysadmin returns true when the user is a cross-project super admin
// (ADMIN role with no project binding).
func (u *User) IsSysadmin() bool {
	return u.Role == RoleAdmin && u.ProjectID == ""
}

// Can checks whether this user is authorized to act in a ward.
// SYSADMIN and project ADMIN can act in any ward within their project;
// other roles must be scoped to a ward they belong to.
func (u *User) Can(wardID string) bool {
	if u.Role == RoleAdmin {
		return true
	}
	for _, w := range u.WardIDs {
		if w == wardID {
			return true
		}
	}
	return false
}

// Project is the domain model for a multi-tenant project.
type Project struct {
	ID        string
	Name      string
	Slug      string
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}
