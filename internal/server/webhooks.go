package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	svix "github.com/svix/svix-webhooks/go"

	"github.com/guillermoBallester/isthmus/internal/adapter/store"
	"github.com/guillermoBallester/isthmus/internal/clerk"
)

// WebhookHandler processes Clerk webhook events.
type WebhookHandler struct {
	pool    *pgxpool.Pool
	queries *store.Queries
	wh      *svix.Webhook
	logger  *slog.Logger
}

// NewWebhookHandler creates a new Clerk webhook handler with Svix signature verification.
func NewWebhookHandler(pool *pgxpool.Pool, queries *store.Queries, webhookSecret string, logger *slog.Logger) *WebhookHandler {
	wh, err := svix.NewWebhook(webhookSecret)
	if err != nil {
		// This only fails if the secret is malformed.
		logger.Error("invalid webhook secret", slog.String("error", err.Error()))
		return nil
	}
	return &WebhookHandler{
		pool:    pool,
		queries: queries,
		wh:      wh,
		logger:  logger,
	}
}

// HandleClerkWebhook returns an HTTP handler for POST /api/webhooks/clerk.
func (h *WebhookHandler) HandleClerkWebhook() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		// Verify Svix signature.
		headers := http.Header{
			"svix-id":        r.Header.Values("svix-id"),
			"svix-timestamp": r.Header.Values("svix-timestamp"),
			"svix-signature": r.Header.Values("svix-signature"),
		}
		if err := h.wh.Verify(body, headers); err != nil {
			h.logger.Warn("webhook signature verification failed",
				slog.String("error", err.Error()),
			)
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}

		var event clerk.WebhookEvent
		if err := json.Unmarshal(body, &event); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		h.logger.Info("clerk webhook received", slog.String("type", event.Type))

		switch event.Type {
		case "user.created":
			if err := h.handleUserCreated(r, event.Data); err != nil {
				h.logger.Error("failed to handle user.created",
					slog.String("error", err.Error()),
				)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
		case "organization.created":
			if err := h.handleOrgCreated(r, event.Data); err != nil {
				h.logger.Error("failed to handle organization.created",
					slog.String("error", err.Error()),
				)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
		default:
			h.logger.Debug("ignoring unhandled webhook event", slog.String("type", event.Type))
		}

		w.WriteHeader(http.StatusOK)
	}
}

func (h *WebhookHandler) handleUserCreated(r *http.Request, data json.RawMessage) error {
	var d clerk.UserCreatedData
	if err := json.Unmarshal(data, &d); err != nil {
		return fmt.Errorf("unmarshal user data: %w", err)
	}

	if len(d.EmailAddresses) == 0 {
		return fmt.Errorf("user %s has no email addresses", d.ID)
	}
	email := d.EmailAddresses[0].EmailAddress

	ctx := r.Context()

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	qtx := h.queries.WithTx(tx)

	// Create user (idempotent — ignore conflict).
	user, err := qtx.CreateUser(ctx, store.CreateUserParams{
		ClerkUserID: d.ID,
		Email:       email,
	})
	if err != nil {
		// Check if user already exists.
		existing, lookupErr := qtx.GetUserByClerkID(ctx, d.ID)
		if lookupErr != nil {
			return fmt.Errorf("create user: %w", err)
		}
		user = existing
	}

	// Create personal workspace.
	ws, err := qtx.CreateWorkspace(ctx, store.CreateWorkspaceParams{
		OwnerUserID: user.ID,
		Name:        "Personal",
		IsPersonal:  true,
	})
	if err != nil {
		// Workspace may already exist if webhook is replayed.
		existing, lookupErr := qtx.GetPersonalWorkspace(ctx, user.ID)
		if lookupErr != nil {
			return fmt.Errorf("create personal workspace: %w", err)
		}
		ws = existing
	}

	// Add user as owner of their personal workspace.
	if err := qtx.AddWorkspaceMember(ctx, store.AddWorkspaceMemberParams{
		WorkspaceID: ws.ID,
		UserID:      user.ID,
		Role:        "owner",
	}); err != nil {
		return fmt.Errorf("add workspace member: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	h.logger.Info("user created with personal workspace",
		slog.String("clerk_user_id", d.ID),
		slog.String("email", email),
	)

	return nil
}

func (h *WebhookHandler) handleOrgCreated(r *http.Request, data json.RawMessage) error {
	var d clerk.OrganizationCreatedData
	if err := json.Unmarshal(data, &d); err != nil {
		return fmt.Errorf("unmarshal org data: %w", err)
	}

	ctx := r.Context()

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	qtx := h.queries.WithTx(tx)

	// Look up the creator.
	creator, err := qtx.GetUserByClerkID(ctx, d.CreatedBy)
	if err != nil {
		return fmt.Errorf("look up org creator %s: %w", d.CreatedBy, err)
	}

	// Create team workspace.
	ws, err := qtx.CreateWorkspace(ctx, store.CreateWorkspaceParams{
		ClerkOrgID: pgtype.Text{String: d.ID, Valid: true},
		Name:       d.Name,
		IsPersonal: false,
	})
	if err != nil {
		// Workspace may already exist if webhook is replayed.
		existing, lookupErr := qtx.GetWorkspaceByClerkOrgID(ctx, pgtype.Text{String: d.ID, Valid: true})
		if lookupErr != nil {
			return fmt.Errorf("create team workspace: %w", err)
		}
		ws = existing
	}

	// Add creator as owner.
	if err := qtx.AddWorkspaceMember(ctx, store.AddWorkspaceMemberParams{
		WorkspaceID: ws.ID,
		UserID:      creator.ID,
		Role:        "owner",
	}); err != nil {
		return fmt.Errorf("add org owner: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	h.logger.Info("organization workspace created",
		slog.String("clerk_org_id", d.ID),
		slog.String("name", d.Name),
	)

	return nil
}
