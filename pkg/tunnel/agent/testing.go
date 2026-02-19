package agent

import "context"

// HasSession returns true if the agent has a tracked session with the given ID.
func (a *Agent) HasSession(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.sessions[id]
	return ok
}

// SessionCount returns the number of tracked sessions.
func (a *Agent) SessionCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.sessions)
}

// CleanStaleSessions exports cleanStaleSessions for testing.
func (a *Agent) CleanStaleSessions(ctx context.Context) {
	a.cleanStaleSessions(ctx)
}

// SetDraining sets the draining flag directly.
func (a *Agent) SetDraining(v bool) {
	a.draining.Store(v)
}

// ConnectAndServe exports connectAndServe for testing.
func (a *Agent) ConnectAndServe(ctx context.Context) error {
	return a.connectAndServe(ctx)
}
