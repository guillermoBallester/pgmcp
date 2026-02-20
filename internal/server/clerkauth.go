package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/clerk/clerk-sdk-go/v2/jwt"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/guillermoBallester/isthmus/internal/store"
)

type contextKey int

const (
	ctxKeyUser contextKey = iota
	ctxKeyWorkspaceMember
)

// UserFromContext returns the authenticated internal user from the request context.
func UserFromContext(ctx context.Context) (store.User, bool) {
	u, ok := ctx.Value(ctxKeyUser).(store.User)
	return u, ok
}

// MemberFromContext returns the workspace membership from the request context.
func MemberFromContext(ctx context.Context) (store.WorkspaceMember, bool) {
	m, ok := ctx.Value(ctxKeyWorkspaceMember).(store.WorkspaceMember)
	return m, ok
}

// clerkJWTAuth verifies the Clerk session JWT and injects the internal User into context.
func (s *Server) clerkJWTAuth(queries *store.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
				return
			}
			token := strings.TrimPrefix(header, "Bearer ")

			claims, err := jwt.Verify(r.Context(), &jwt.VerifyParams{
				Token: token,
			})
			if err != nil {
				s.logger.Debug("clerk JWT verification failed",
					slog.String("error", err.Error()),
				)
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			clerkUserID := claims.Subject

			user, err := queries.GetUserByClerkID(r.Context(), clerkUserID)
			if err != nil {
				s.logger.Warn("clerk user not found in database",
					slog.String("clerk_user_id", clerkUserID),
				)
				http.Error(w, `{"error":"user not found"}`, http.StatusForbidden)
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyUser, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// workspaceMemberAuth checks that the authenticated user is a member of the
// workspace identified by {workspace_id} in the URL path.
func (s *Server) workspaceMemberAuth(queries *store.Queries) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			wsIDStr := chi.URLParam(r, "workspace_id")
			var wsID pgtype.UUID
			if err := wsID.Scan(wsIDStr); err != nil {
				http.Error(w, `{"error":"invalid workspace_id"}`, http.StatusBadRequest)
				return
			}

			member, err := queries.GetWorkspaceMember(r.Context(), store.GetWorkspaceMemberParams{
				WorkspaceID: wsID,
				UserID:      user.ID,
			})
			if err != nil {
				http.Error(w, `{"error":"not a member of this workspace"}`, http.StatusForbidden)
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyWorkspaceMember, member)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
