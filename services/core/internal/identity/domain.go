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

// User is the domain model. It decouples the store from proto types.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	DisplayName  string
	Role         Role
	WardIDs      []string
	Active       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Can checks whether this user is authorized to perform an action in a ward.
// Admins can act in any ward; other roles must be scoped to a ward they belong to.
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
