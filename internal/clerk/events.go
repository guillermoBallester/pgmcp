package clerk

import "encoding/json"

// WebhookEvent represents a Clerk webhook event envelope.
type WebhookEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// UserCreatedData is the payload for "user.created" events.
type UserCreatedData struct {
	ID             string         `json:"id"`
	EmailAddresses []EmailAddress `json:"email_addresses"`
}

// EmailAddress is a nested object within Clerk user data.
type EmailAddress struct {
	EmailAddress string `json:"email_address"`
}

// OrganizationCreatedData is the payload for "organization.created" events.
type OrganizationCreatedData struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedBy string `json:"created_by"`
}
