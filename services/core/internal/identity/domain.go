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
	ProjectID    *string // nil = SYSADMIN (cross-project)
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// IsSysadmin returns true when the user is a cross-project super admin
// (ADMIN role with no project binding).
func (u *User) IsSysadmin() bool {
	return u.Role == RoleAdmin && (u.ProjectID == nil || *u.ProjectID == "")
}

// Can checks whether this user is authorized to act in a ward.
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

// ProjectIDStr returns the project ID as a string, or empty for SYSADMIN.
func (u *User) ProjectIDStr() string {
	if u.ProjectID == nil {
		return ""
	}
	return *u.ProjectID
}

// Project is the domain model for a multi-tenant project.
type Project struct {
	ID          string
	Code        string
	Name        string
	DisplayName string
	Slug        string
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
