package core

import (
	"sync"
	"time"
)

// PresenceSession represents an active client session participating in a presence room.
type PresenceSession struct {
	SessionId string         `json:"sessionId"`
	UserId    string         `json:"userId,omitempty"`
	TenantId  string         `json:"tenantId,omitempty"`
	State     map[string]any `json:"state"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// PresenceManager manages ephemeral in-memory presence states across channels.
type PresenceManager struct {
	mu       sync.RWMutex
	channels map[string]map[string]*PresenceSession // channel -> sessionId -> session
}

// NewPresenceManager creates a new instance of PresenceManager.
func NewPresenceManager() *PresenceManager {
	return &PresenceManager{
		channels: make(map[string]map[string]*PresenceSession),
	}
}

// Track registers or updates a client's presence state in a channel.
func (pm *PresenceManager) Track(channel, sessionId, userId, tenantId string, state map[string]any) *PresenceSession {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	sessions, ok := pm.channels[channel]
	if !ok {
		sessions = make(map[string]*PresenceSession)
		pm.channels[channel] = sessions
	}

	session := &PresenceSession{
		SessionId: sessionId,
		UserId:    userId,
		TenantId:  tenantId,
		State:     state,
		UpdatedAt: time.Now(),
	}
	sessions[sessionId] = session

	return session
}

// Untrack removes a client's presence from a specific channel.
func (pm *PresenceManager) Untrack(channel, sessionId string) (*PresenceSession, bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	sessions, ok := pm.channels[channel]
	if !ok {
		return nil, false
	}

	session, exists := sessions[sessionId]
	if exists {
		delete(sessions, sessionId)
		if len(sessions) == 0 {
			delete(pm.channels, channel)
		}
	}

	return session, exists
}

// UntrackSession removes all presence entries for a given session ID across all channels
// and returns the list of affected channel names.
func (pm *PresenceManager) UntrackSession(sessionId string) []string {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	var affected []string
	for channel, sessions := range pm.channels {
		if _, exists := sessions[sessionId]; exists {
			delete(sessions, sessionId)
			affected = append(affected, channel)
			if len(sessions) == 0 {
				delete(pm.channels, channel)
			}
		}
	}

	return affected
}

// List returns a snapshot of all active presence sessions in a channel.
func (pm *PresenceManager) List(channel string) []*PresenceSession {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	sessions, ok := pm.channels[channel]
	if !ok {
		return nil
	}

	res := make([]*PresenceSession, 0, len(sessions))
	for _, s := range sessions {
		res = append(res, s)
	}

	return res
}

// ListForTenant returns active presence sessions in a channel filtered by tenantId (if non-empty).
func (pm *PresenceManager) ListForTenant(channel, tenantId string) []*PresenceSession {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	sessions, ok := pm.channels[channel]
	if !ok {
		return nil
	}

	res := make([]*PresenceSession, 0, len(sessions))
	for _, s := range sessions {
		if tenantId == "" || s.TenantId == "" || s.TenantId == tenantId {
			res = append(res, s)
		}
	}

	return res
}
