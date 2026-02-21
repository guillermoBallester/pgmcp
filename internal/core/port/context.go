package port

import "context"

type contextKey int

const authResultKey contextKey = iota

// ContextWithAuth attaches an AuthResult to the context.
func ContextWithAuth(ctx context.Context, result *AuthResult) context.Context {
	return context.WithValue(ctx, authResultKey, result)
}

// AuthFromContext extracts the AuthResult from the context.
// Returns nil if no AuthResult is present.
func AuthFromContext(ctx context.Context) *AuthResult {
	result, _ := ctx.Value(authResultKey).(*AuthResult)
	return result
}
