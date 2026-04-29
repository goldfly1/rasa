package pool

import (
	"sync"
	"time"
)

// AgentInfo represents a live agent known to the pool.
type AgentInfo struct {
	AgentID       string    `json:"agent_id"`
	SoulID        string    `json:"soul_id"`
	State         string    `json:"state"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	RegisteredAt  time.Time `json:"registered_at"`
}

// AgentRegistry holds the set of known agents keyed by agent_id.
type AgentRegistry struct {
	mu     sync.RWMutex
	agents map[string]*AgentInfo
	bySoul map[string]map[string]struct{} // soul_id → set of agent_id
}

func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		agents: make(map[string]*AgentInfo),
		bySoul: make(map[string]map[string]struct{}),
	}
}

// Upsert records a heartbeat, adding the agent if unseen.
func (r *AgentRegistry) Upsert(agentID, soulID, state string) *AgentInfo {
	r.mu.Lock()
	defer r.mu.Unlock()

	info, ok := r.agents[agentID]
	if ok {
		info.State = state
		info.LastHeartbeat = time.Now()
		if info.SoulID != soulID {
			r.removeSoulIndex(info.SoulID, agentID)
			info.SoulID = soulID
			r.addSoulIndex(soulID, agentID)
		}
		return info
	}

	info = &AgentInfo{
		AgentID:       agentID,
		SoulID:        soulID,
		State:         state,
		LastHeartbeat: time.Now(),
		RegisteredAt:  time.Now(),
	}
	r.agents[agentID] = info
	r.addSoulIndex(soulID, agentID)
	return info
}

// FindBySoul returns all agent IDs registered for a given soul_id.
func (r *AgentRegistry) FindBySoul(soulID string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	set, ok := r.bySoul[soulID]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}

// RemoveDead removes agents whose last heartbeat exceeds the deadline.
func (r *AgentRegistry) RemoveDead(deadline time.Time) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	var removed []string
	for id, info := range r.agents {
		if info.LastHeartbeat.Before(deadline) {
			removed = append(removed, id)
			r.removeSoulIndex(info.SoulID, id)
			delete(r.agents, id)
		}
	}
	return removed
}

// CountByState returns the number of agents in a given state.
func (r *AgentRegistry) CountByState(state string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n := 0
	for _, info := range r.agents {
		if info.State == state {
			n++
		}
	}
	return n
}

// Count returns the total number of registered agents.
func (r *AgentRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.agents)
}

// Get returns the AgentInfo for an agent_id, or nil.
func (r *AgentRegistry) Get(agentID string) *AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agents[agentID]
}

// List returns all registered agents.
func (r *AgentRegistry) List() []*AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*AgentInfo, 0, len(r.agents))
	for _, info := range r.agents {
		out = append(out, info)
	}
	return out
}

func (r *AgentRegistry) addSoulIndex(soulID, agentID string) {
	if r.bySoul[soulID] == nil {
		r.bySoul[soulID] = make(map[string]struct{})
	}
	r.bySoul[soulID][agentID] = struct{}{}
}

func (r *AgentRegistry) removeSoulIndex(soulID, agentID string) {
	set := r.bySoul[soulID]
	if set != nil {
		delete(set, agentID)
		if len(set) == 0 {
			delete(r.bySoul, soulID)
		}
	}
}
