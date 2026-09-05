package store

import (
	"fmt"
	"sort"
	"time"
)

const sharedHistoryRetention = 30 * 24 * time.Hour
const retiredKeyRetention = 30 * 24 * time.Hour
const maxRetiredKeys = 10000

type retiredKey struct {
	Project   string    `json:"project"`
	AgentID   string    `json:"agent_id"`
	Key       string    `json:"key"`
	MessageID string    `json:"message_id"`
	Expires   time.Time `json:"expires"`
}

type MessageQuota struct {
	Limit           int  `json:"limit"`
	Used            int  `json:"used"`
	ReservedResults int  `json:"reserved_results"`
	Available       int  `json:"available"`
	RetiredKeys     int  `json:"retired_keys"`
	Warning         bool `json:"warning"`
	Overcommitted   bool `json:"overcommitted"`
}

func (s *Store) MessageQuota(project string) MessageQuota {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.messageQuota(project)
}

func (s *Store) messageQuota(project string) MessageQuota {
	q := MessageQuota{Limit: maxProjectMessages}
	now := s.now().UTC()
	for _, m := range s.state.Messages {
		if m.Project == project {
			q.Used++
		}
		if owner, ok := s.state.Sessions[m.AcceptedBy]; ok && owner.Project == project && s.acceptanceLive(m, now) {
			q.ReservedResults++
		}
	}
	for _, key := range s.state.RetiredKeys {
		if key.Project == project && key.Expires.After(now) {
			q.RetiredKeys++
		}
	}
	effective := q.Used + q.ReservedResults
	q.Available = max(0, q.Limit-effective)
	q.Warning = effective >= q.Limit*80/100 || q.RetiredKeys >= maxRetiredKeys*80/100
	q.Overcommitted = effective > q.Limit
	return q
}

func expiredShared(m Message, now time.Time) bool {
	// Only explicit Fabric informational messages opt into age-based retention.
	// Legacy pending correspondence and unfinished work never expire implicitly.
	return m.FromAgentID != "" && (m.Kind == "message" || m.Kind == "broadcast" || m.Kind == "result") &&
		m.ToAgentID == "" && m.ToSessionID == "" && m.To == "" &&
		!m.Created.IsZero() && !m.Created.Add(sharedHistoryRetention).After(now)
}

func messagePending(m Message) bool {
	if m.Kind == "request" {
		return m.Status != "completed"
	}
	return m.Status == "pending" || m.Status == "accepted" || m.AckRequired && m.Status != "acknowledged"
}

func (s *Store) evictable(m Message, now time.Time) bool {
	if m.Kind == "request" && m.Status == "completed" {
		for _, result := range s.state.Messages {
			if result.ID == m.ResultMessageID {
				return !messagePending(result)
			}
		}
		return true
	}
	return !messagePending(m) || expiredShared(m, now)
}

// makeMessageRoom admits one message or reserves one completion slot. Select
// every eviction before mutating state, so a refused operation loses no history.
func (s *Store) makeMessageRoom(project string) error {
	now := s.now().UTC()
	quota := s.messageQuota(project)
	needed := quota.Used + quota.ReservedResults + 1 - quota.Limit
	candidates := []int{}
	for i, m := range s.state.Messages {
		if m.Project == project && s.evictable(m, now) {
			candidates = append(candidates, i)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return s.state.Messages[candidates[i]].Created.Before(s.state.Messages[candidates[j]].Created)
	})
	removed := map[int]bool{}
	retired := []retiredKey{}
	keyCount := quota.RetiredKeys
	for _, i := range candidates {
		m := s.state.Messages[i]
		if needed <= 0 && !expiredShared(m, now) {
			continue
		}
		if m.IdempotencyKey != "" && m.FromAgentID != "" {
			if keyCount >= maxRetiredKeys {
				continue
			}
			retired = append(retired, retiredKey{m.Project, m.FromAgentID, m.IdempotencyKey, m.ID, now.Add(retiredKeyRetention)})
			keyCount++
		}
		removed[i] = true
		needed--
	}
	if needed > 0 {
		return fmt.Errorf("project message quota exceeded (used=%d reserved_results=%d limit=%d retired_keys=%d); pending work preserved; acknowledge processed direct messages or wait for shared history/key retention", quota.Used, quota.ReservedResults, quota.Limit, quota.RetiredKeys)
	}
	messages := make([]Message, 0, len(s.state.Messages)-len(removed))
	for i, m := range s.state.Messages {
		if !removed[i] {
			messages = append(messages, m)
		}
	}
	keys := make([]retiredKey, 0, len(s.state.RetiredKeys)+len(retired))
	for _, key := range s.state.RetiredKeys {
		if key.Expires.After(now) {
			keys = append(keys, key)
		}
	}
	s.state.Messages = messages
	s.state.RetiredKeys = append(keys, retired...)
	return nil
}
