package identity

import "context"

// UserStore is the narrow user-lookup interface consumed by the auth service.
// The concrete Store already satisfies this interface.
type UserStore interface {
	GetByUsername(ctx context.Context, username string) (*User, error)
	GetByCardToken(ctx context.Context, token string) (*User, error)
	GetByID(ctx context.Context, id string) (*User, error)
}
