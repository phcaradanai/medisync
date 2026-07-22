// Package auth provides shared authorization types and context helpers
// consumed by all bounded contexts. It eliminates the per-context
// duplication of TokenClaims, extractBearer, and requireAdmin.
package auth

import "context"

// Claims carries the canonical authorization payload extracted from a JWT.
// All bounded contexts import this single type; no context defines its own copy.
type Claims struct {
	Subject   string   // user ID
	Role      string   // ADMIN, PHARMACIST, NURSE, REFILLER
	ProjectID string   // empty = SYSADMIN (cross-project)
	WardIDs   []string // ward scoping
}

// ── Context ────────────────────────────────────────────────────────

type ctxKey struct{}

// SetClaims stores claims in the context.
func SetClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// GetClaims retrieves claims from the context. The bool is false when
// no claims were injected (middleware not wired).
func GetClaims(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(ctxKey{}).(Claims)
	return c, ok
}

// ── Authorization helpers ──────────────────────────────────────────

// IsSysadmin returns true when the caller has ADMIN role and no project binding.
func (c Claims) IsSysadmin() bool {
	return c.Role == "ADMIN" && c.ProjectID == ""
}

// IsProjectAdmin returns true when the caller is an ADMIN bound to a project.
func (c Claims) IsProjectAdmin() bool {
	return c.Role == "ADMIN" && c.ProjectID != ""
}

// HasProjectAccess returns true when the caller may access the given project.
// SYSADMIN can access all projects; others must match their own project.
func (c Claims) HasProjectAccess(projectID string) bool {
	if c.IsSysadmin() {
		return true
	}
	return c.ProjectID == projectID
}

// CanAccessWard returns true when the caller may act in the given ward.
// SYSADMIN sees all wards; project ADMIN sees all wards in their project;
// other roles are scoped by WardIDs.
func (c Claims) CanAccessWard(wardID string) bool {
	if c.IsSysadmin() || c.IsProjectAdmin() {
		return true
	}
	for _, w := range c.WardIDs {
		if w == wardID {
			return true
		}
	}
	return false
}
