package store

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const LeaseDuration = 2 * time.Minute

type Registration struct {
	MachineID       string `json:"machine_id,omitempty"`
	AgentKey        string `json:"agent_key,omitempty"`
	ProjectName     string `json:"project_name,omitempty"`
	Name            string `json:"name,omitempty"`
	Client          string `json:"client_type,omitempty"`
	NativeSessionID string `json:"native_session_id,omitempty"`
	NativeThreadID  string `json:"native_thread_id,omitempty"`
	Branch          string `json:"branch,omitempty"`
	Head            string `json:"head,omitempty"`
	Dirty           bool   `json:"dirty,omitempty"`
}

type DiscoveryFilter struct {
	AgentID     string `json:"agent_id,omitempty"`
	ClientType  string `json:"client_type,omitempty"`
	ProjectID   string `json:"project_id,omitempty"`
	ProjectName string `json:"project_name,omitempty"`
	SpaceID     string `json:"space_id,omitempty"`
	MachineID   string `json:"machine_id,omitempty"`
	Capability  string `json:"capability,omitempty"`
	State       string `json:"state,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type Agent struct {
	ID          string    `json:"agent_id"`
	MachineID   string    `json:"machine_id"`
	Client      string    `json:"client_type"`
	ProjectID   string    `json:"project_id"`
	DisplayName string    `json:"display_name"`
	State       string    `json:"state"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	Sessions    []Session `json:"sessions"`
}

type agentRecord struct {
	ID          string    `json:"id"`
	MachineID   string    `json:"machine_id"`
	AgentKey    string    `json:"agent_key"`
	Client      string    `json:"client_type"`
	ProjectID   string    `json:"project_id"`
	DisplayName string    `json:"display_name"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

type Project struct {
	ID          string    `json:"project_id"`
	DisplayName string    `json:"display_name"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

type Space struct {
	ID        string `json:"space_id"`
	Read      bool   `json:"read"`
	Send      bool   `json:"send"`
	Broadcast bool   `json:"broadcast"`
}

type SendInput struct {
	SessionID        string `json:"session_id"`
	ToAgentID        string `json:"to_agent_id,omitempty"`
	ToProjectID      string `json:"to_project_id,omitempty"`
	ToSessionID      string `json:"to_session_id,omitempty"`
	SpaceID          string `json:"space_id,omitempty"`
	ThreadID         string `json:"thread_id,omitempty"`
	CorrelationID    string `json:"correlation_id,omitempty"`
	Body             string `json:"body"`
	Kind             string `json:"kind,omitempty"`
	IdempotencyKey   string `json:"idempotency_key,omitempty"`
	AuthorizationRef string `json:"authorization_ref,omitempty"`
	AckRequired      bool   `json:"ack_required,omitempty"`
}

func (s *Store) globalAllowed(project string) bool { return !s.state.GlobalDeny[project] }

func (s *Store) SetGlobalAccess(project string, allowed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateText("project", project, 120); err != nil {
		return err
	}
	if project == "" {
		return errors.New("project is required")
	}
	if allowed {
		delete(s.state.GlobalDeny, project)
	} else {
		s.state.GlobalDeny[project] = true
	}
	return s.commit()
}

func (s *Store) ReconcileSession(project string, in Registration) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateRegistration(project, in); err != nil {
		return Session{}, err
	}
	if project == "" || in.Client == "" || in.NativeSessionID == "" {
		return Session{}, errors.New("project, client_type, and native_session_id are required")
	}
	if in.MachineID == "" {
		in.MachineID = "local"
	}
	if in.AgentKey == "" {
		in.AgentKey = "main"
	}
	if in.ProjectName == "" {
		in.ProjectName = project
	}
	if in.Name == "" {
		in.Name = in.AgentKey
	}
	now := s.now().UTC()
	var agent agentRecord
	for _, candidate := range s.state.Agents {
		if candidate.ProjectID == project && candidate.MachineID == in.MachineID && candidate.Client == in.Client && candidate.AgentKey == in.AgentKey {
			agent = candidate
			break
		}
	}
	if agent.ID == "" {
		count := 0
		for _, candidate := range s.state.Agents {
			if candidate.ProjectID == project {
				count++
			}
		}
		if count >= maxProjectAgents {
			return Session{}, errors.New("project agent quota exceeded")
		}
		agent = agentRecord{ID: id(), MachineID: in.MachineID, AgentKey: in.AgentKey, Client: in.Client, ProjectID: project}
	}
	for sessionID, current := range s.state.Sessions {
		if current.AgentID != agent.ID || current.State != "running" {
			continue
		}
		if !current.LeaseExpires.After(now) {
			current.State = "stale"
			current.Updated = now
			s.state.Sessions[sessionID] = current
			continue
		}
		if current.NativeSessionID != in.NativeSessionID {
			return Session{}, fmt.Errorf("agent identity is active in native session %q; set a distinct agent_key to run concurrently", current.NativeSessionID)
		}
		current.Name, current.ProjectName = in.Name, in.ProjectName
		s.renewRequestLeases(current.ID, now)
		current.NativeThreadID, current.Branch, current.Head, current.Dirty = in.NativeThreadID, in.Branch, in.Head, in.Dirty
		current.Updated, current.LeaseExpires = now, now.Add(LeaseDuration)
		agent.DisplayName, agent.LastSeenAt = in.Name, now
		s.state.Agents[agent.ID] = agent
		s.state.Projects[project] = Project{ID: project, DisplayName: in.ProjectName, LastSeenAt: now}
		s.state.Sessions[current.ID] = current
		return current, s.commit()
	}
	if err := s.makeSessionRoom(project); err != nil {
		return Session{}, err
	}
	agent.DisplayName, agent.LastSeenAt = in.Name, now
	s.state.Agents[agent.ID] = agent
	s.state.Projects[project] = Project{ID: project, DisplayName: in.ProjectName, LastSeenAt: now}
	v := Session{
		ID: id(), Project: project, Name: in.Name, Client: in.Client, State: "running", Updated: now,
		AgentID: agent.ID, MachineID: in.MachineID, NativeSessionID: in.NativeSessionID,
		NativeThreadID: in.NativeThreadID, ProjectName: in.ProjectName, Branch: in.Branch,
		Head: in.Head, Dirty: in.Dirty, LeaseExpires: now.Add(LeaseDuration),
	}
	s.state.Sessions[v.ID] = v
	return v, s.commit()
}

func validateText(name, value string, max int) error {
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) || len(value) > max {
		return fmt.Errorf("%s must be valid text of at most %d bytes", name, max)
	}
	return nil
}

func validateRegistration(project string, in Registration) error {
	fields := []struct {
		name, value string
		max         int
	}{
		{"project", project, 120}, {"machine_id", in.MachineID, 120}, {"agent_key", in.AgentKey, 120},
		{"project_name", in.ProjectName, 120}, {"name", in.Name, 120}, {"client_type", in.Client, 120},
		{"native_session_id", in.NativeSessionID, 256}, {"native_thread_id", in.NativeThreadID, 256},
		{"branch", in.Branch, 256}, {"head", in.Head, 256},
	}
	for _, field := range fields {
		if err := validateText(field.name, field.value, field.max); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) makeSessionRoom(project string) error {
	count, oldest := 0, ""
	for sessionID, session := range s.state.Sessions {
		if session.Project != project {
			continue
		}
		count++
		if (session.State == "ended" || session.State == "stale" || session.AgentID != "" && !session.LeaseExpires.After(s.now().UTC())) && (oldest == "" || session.Updated.Before(s.state.Sessions[oldest].Updated)) {
			oldest = sessionID
		}
	}
	if count < maxProjectSessions {
		return nil
	}
	if oldest == "" {
		return errors.New("project session quota exceeded")
	}
	delete(s.state.Sessions, oldest)
	return nil
}

func (s *Store) Renew(project, session string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	v, ok := s.state.Sessions[session]
	if !ok || v.Project != project || v.AgentID == "" || v.State != "running" || !v.LeaseExpires.After(now) {
		return Session{}, errors.New("active leased session not found; reconcile the native session")
	}
	s.renewRequestLeases(session, now)
	v.Updated, v.LeaseExpires = now, now.Add(LeaseDuration)
	s.state.Sessions[session] = v
	agent := s.state.Agents[v.AgentID]
	agent.LastSeenAt = now
	s.state.Agents[v.AgentID] = agent
	projectRecord := s.state.Projects[project]
	projectRecord.LastSeenAt = now
	s.state.Projects[project] = projectRecord
	return v, s.commit()
}

func (s *Store) Discover(project string, filter DiscoveryFilter) ([]Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fields := []struct {
		name, value string
		max         int
	}{
		{"project", project, 120}, {"agent_id", filter.AgentID, 120}, {"client_type", filter.ClientType, 120},
		{"project_id", filter.ProjectID, 120}, {"project_name", filter.ProjectName, 120}, {"space_id", filter.SpaceID, 256},
		{"machine_id", filter.MachineID, 120}, {"capability", filter.Capability, 120}, {"state", filter.State, 32},
	}
	for _, field := range fields {
		if err := validateText(field.name, field.value, field.max); err != nil {
			return nil, err
		}
	}
	if filter.SpaceID != "" && filter.SpaceID != "global://default" && filter.SpaceID != "project://"+project {
		return nil, errors.New("unknown space")
	}
	if strings.HasPrefix(filter.SpaceID, "global://") && !s.globalAllowed(project) {
		return []Agent{}, nil
	}
	now := s.now().UTC()
	out := []Agent{}
	for _, record := range s.state.Agents {
		if filter.SpaceID == "project://"+project && record.ProjectID != project {
			continue
		}
		if record.ProjectID != project && (!s.globalAllowed(project) || !s.globalAllowed(record.ProjectID)) {
			continue
		}
		if filter.AgentID != "" && filter.AgentID != record.ID || filter.ClientType != "" && filter.ClientType != record.Client || filter.ProjectID != "" && filter.ProjectID != record.ProjectID || filter.ProjectName != "" && filter.ProjectName != s.state.Projects[record.ProjectID].DisplayName || filter.MachineID != "" && filter.MachineID != record.MachineID || filter.Capability != "" {
			continue
		}
		agent := Agent{ID: record.ID, MachineID: record.MachineID, Client: record.Client, ProjectID: record.ProjectID, DisplayName: record.DisplayName, State: "offline", LastSeenAt: record.LastSeenAt, Sessions: []Session{}}
		for _, session := range s.state.Sessions {
			if session.AgentID == record.ID && session.State == "running" && session.LeaseExpires.After(now) {
				agent.State = "online"
				agent.Sessions = append(agent.Sessions, session)
			}
		}
		if filter.State != "" && filter.State != agent.State {
			continue
		}
		sort.Slice(agent.Sessions, func(i, j int) bool { return agent.Sessions[i].Updated.After(agent.Sessions[j].Updated) })
		if len(agent.Sessions) > 10 {
			agent.Sessions = append([]Session(nil), agent.Sessions[:10]...)
		}
		out = append(out, agent)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeenAt.After(out[j].LastSeenAt) })
	if len(out) > clamp(filter.Limit) {
		out = out[:clamp(filter.Limit)]
	}
	return out, nil
}

func (s *Store) Projects(project, name string, limit int) []Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Project{}
	for _, candidate := range s.state.Projects {
		if candidate.ID != project && (!s.globalAllowed(project) || !s.globalAllowed(candidate.ID)) {
			continue
		}
		if name == "" || strings.Contains(strings.ToLower(candidate.DisplayName), strings.ToLower(name)) {
			out = append(out, candidate)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeenAt.After(out[j].LastSeenAt) })
	if len(out) > clamp(limit) {
		out = out[:clamp(limit)]
	}
	return out
}

func (s *Store) Spaces(project string) []Space {
	s.mu.Lock()
	defer s.mu.Unlock()
	allowed := s.globalAllowed(project)
	return []Space{{ID: "project://" + project, Read: true, Send: true, Broadcast: true}, {ID: "global://default", Read: allowed, Send: allowed, Broadcast: allowed}}
}

func (s *Store) messageVisible(message Message, session Session) bool {
	if message.Scope != "global" {
		return message.Project == session.Project
	}
	return s.globalAllowed(message.Project) && s.globalAllowed(session.Project)
}

func (s *Store) messageVisibleToProject(message Message, project string) bool {
	if message.Scope != "global" {
		return message.Project == project
	}
	if !s.globalAllowed(message.Project) || !s.globalAllowed(project) {
		return false
	}
	if message.Project == project || message.ToProjectID == project {
		return true
	}
	if message.ToAgentID != "" {
		return s.state.Agents[message.ToAgentID].ProjectID == project
	}
	if message.ToSessionID != "" {
		return s.state.Sessions[message.ToSessionID].Project == project
	}
	return message.ToAgentID == "" && message.ToProjectID == "" && message.ToSessionID == ""
}

func (s *Store) SendMessage(project string, in SendInput) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fields := []struct {
		name, value string
		max         int
	}{
		{"project", project, 120}, {"session_id", in.SessionID, 120}, {"to_agent_id", in.ToAgentID, 120},
		{"to_project_id", in.ToProjectID, 120}, {"to_session_id", in.ToSessionID, 120}, {"space_id", in.SpaceID, 256},
		{"thread_id", in.ThreadID, 256}, {"correlation_id", in.CorrelationID, 256}, {"body", in.Body, 65536},
		{"kind", in.Kind, 16}, {"idempotency_key", in.IdempotencyKey, 256}, {"authorization_ref", in.AuthorizationRef, 256},
	}
	for _, field := range fields {
		if err := validateText(field.name, field.value, field.max); err != nil {
			return Message{}, err
		}
	}
	return s.sendMessageLocked(project, in)
}

func (s *Store) sendMessageLocked(project string, in SendInput) (Message, error) {
	from, ok := s.state.Sessions[in.SessionID]
	if !ok || !s.validSession(project, in.SessionID) || from.AgentID == "" {
		return Message{}, errors.New("active stable session not found")
	}
	if in.ToSessionID != "" && (in.ToAgentID != "" || in.ToProjectID != "") {
		return Message{}, errors.New("to_session_id is mutually exclusive with stable destinations")
	}
	if in.Kind == "" {
		in.Kind = "message"
	}
	if in.Kind != "message" && in.Kind != "request" && in.Kind != "result" && in.Kind != "broadcast" {
		return Message{}, errors.New("kind must be message, request, result, or broadcast")
	}
	if in.Kind == "request" && (in.ToSessionID != "" || in.ToAgentID == "" && in.ToProjectID == "") {
		return Message{}, errors.New("requests require a stable agent or project destination")
	}
	if in.SpaceID == "" {
		in.SpaceID = "project://" + project
	}
	if in.SpaceID != "project://"+project && in.SpaceID != "global://default" {
		return Message{}, errors.New("unknown or unavailable space")
	}
	if in.SpaceID == "global://default" && !s.globalAllowed(project) {
		return Message{}, errors.New("global access denied for project")
	}
	targetProject := project
	if in.ToSessionID != "" {
		target, exists := s.state.Sessions[in.ToSessionID]
		if !exists || target.AgentID == "" || !s.validSession(target.Project, target.ID) {
			return Message{}, errors.New("destination session not found")
		}
		if target.Project != project && (!s.globalAllowed(project) || !s.globalAllowed(target.Project)) {
			return Message{}, errors.New("destination session not found")
		}
		targetProject = target.Project
	}
	if in.ToAgentID != "" {
		target, exists := s.state.Agents[in.ToAgentID]
		if !exists {
			return Message{}, errors.New("destination agent not found")
		}
		if target.ProjectID != project && (!s.globalAllowed(project) || !s.globalAllowed(target.ProjectID)) {
			return Message{}, errors.New("destination agent not found")
		}
		targetProject = target.ProjectID
		if in.ToProjectID != "" && in.ToProjectID != targetProject {
			return Message{}, errors.New("destination agent does not belong to to_project_id")
		}
	}
	if in.ToProjectID != "" {
		if _, exists := s.state.Projects[in.ToProjectID]; !exists || in.ToProjectID != project && (!s.globalAllowed(project) || !s.globalAllowed(in.ToProjectID)) {
			return Message{}, errors.New("destination project not found")
		}
		targetProject = in.ToProjectID
	}
	if targetProject != project {
		if in.SpaceID != "global://default" {
			return Message{}, errors.New("cross-project messages require global://default")
		}
		if !s.globalAllowed(project) || !s.globalAllowed(targetProject) {
			return Message{}, errors.New("global access denied for source or destination project")
		}
	}
	if in.IdempotencyKey != "" {
		for _, existing := range s.state.Messages {
			if existing.FromAgentID != from.AgentID || existing.IdempotencyKey != in.IdempotencyKey {
				continue
			}
			if sameSend(existing, in) {
				return cloneMessage(existing), nil
			}
			return Message{}, errors.New("idempotency key was already used with different message input")
		}
	}
	if err := s.makeMessageRoom(project); err != nil {
		return Message{}, err
	}
	now := s.now().UTC()
	message := Message{
		ID: id(), Project: project, Scope: "project", From: in.SessionID, Body: in.Body,
		AckRequired: in.AckRequired, Created: now, FromAgentID: from.AgentID,
		ToAgentID: in.ToAgentID, ToProjectID: in.ToProjectID, ToSessionID: in.ToSessionID,
		SpaceID: in.SpaceID, Thread: in.ThreadID, CorrelationID: in.CorrelationID,
		IdempotencyKey: in.IdempotencyKey, AuthorizationRef: in.AuthorizationRef, Kind: in.Kind, Status: "pending",
	}
	if in.SpaceID == "global://default" {
		message.Scope = "global"
	}
	if message.Thread == "" {
		message.Thread = message.ID
	}
	s.state.Messages = append(s.state.Messages, message)
	return cloneMessage(message), s.commit()
}

func sameSend(message Message, in SendInput) bool {
	return message.ToAgentID == in.ToAgentID && message.ToProjectID == in.ToProjectID && message.ToSessionID == in.ToSessionID &&
		message.SpaceID == in.SpaceID && (in.ThreadID == "" || message.Thread == in.ThreadID) && message.CorrelationID == in.CorrelationID &&
		message.Body == in.Body && message.Kind == in.Kind && message.AuthorizationRef == in.AuthorizationRef && message.AckRequired == in.AckRequired
}

func (s *Store) makeMessageRoom(project string) error {
	count := 0
	for _, message := range s.state.Messages {
		if message.Project == project {
			count++
		}
	}
	for count >= maxProjectMessages {
		removed := false
		for i, message := range s.state.Messages {
			if message.Project == project && !messagePending(message) {
				s.state.Messages = append(s.state.Messages[:i], s.state.Messages[i+1:]...)
				count--
				removed = true
				break
			}
		}
		if !removed {
			return errors.New("project message quota exceeded; pending messages were preserved")
		}
	}
	return nil
}

func messagePending(message Message) bool {
	if message.Kind == "request" && message.Status == "completed" {
		return false
	}
	// ponytail: project delivery stays pending without a recipient snapshot; add one if project inbox churn reaches quota.
	return message.Status == "pending" || message.Status == "accepted" || message.AckRequired && message.Status != "acknowledged"
}

func cloneMessage(message Message) Message {
	message.Acked = append([]string(nil), message.Acked...)
	return message
}

func (s *Store) PendingRequests(project, session string, limit int) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	own, ok := s.state.Sessions[session]
	if !ok || !s.validSession(project, session) || own.AgentID == "" {
		return nil, errors.New("active stable session not found")
	}
	now := s.now().UTC()
	out := []Message{}
	for i := range s.state.Messages {
		message := s.state.Messages[i]
		if message.Kind != "request" || message.Status == "completed" || !s.messageVisible(message, own) || !addressed(message, own) {
			continue
		}
		if message.Status == "accepted" && !s.acceptanceLive(message, now) {
			message.Status, message.AcceptedBy, message.AcceptanceToken = "pending", "", ""
			message.AcceptanceExpires = time.Time{}
		}
		out = append(out, cloneMessage(message))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	if len(out) > clamp(limit) {
		out = out[:clamp(limit)]
	}
	return out, nil
}

func (s *Store) acceptanceLive(message Message, now time.Time) bool {
	owner, ok := s.state.Sessions[message.AcceptedBy]
	return message.Status == "accepted" && ok && owner.State == "running" && owner.LeaseExpires.After(now) && message.AcceptanceExpires.After(now)
}

func (s *Store) renewRequestLeases(session string, now time.Time) {
	for i := range s.state.Messages {
		message := &s.state.Messages[i]
		if message.AcceptedBy == session && s.acceptanceLive(*message, now) {
			message.AcceptanceExpires = now.Add(LeaseDuration)
		}
	}
}

func (s *Store) RequestAccept(project, session, messageID string) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	own, ok := s.state.Sessions[session]
	if !ok || !s.validSession(project, session) || own.AgentID == "" {
		return Message{}, errors.New("active stable session not found")
	}
	now := s.now().UTC()
	for i := range s.state.Messages {
		message := &s.state.Messages[i]
		if message.ID != messageID || message.Kind != "request" || message.Status == "completed" || !s.messageVisible(*message, own) || !addressed(*message, own) {
			continue
		}
		if s.acceptanceLive(*message, now) {
			if message.AcceptedBy == session {
				return cloneMessage(*message), nil
			}
			return Message{}, fmt.Errorf("request is currently accepted by session %s", message.AcceptedBy)
		}
		message.Status, message.AcceptedBy, message.AcceptanceToken = "accepted", session, id()
		message.AcceptanceExpires = own.LeaseExpires
		return cloneMessage(*message), s.commit()
	}
	return Message{}, errors.New("pending request not found")
}

func (s *Store) RequestComplete(project, session, messageID, token, result string) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, field := range []struct {
		name, value string
		max         int
	}{{"project", project, 120}, {"session_id", session, 120}, {"message_id", messageID, 120}, {"acceptance_token", token, 120}, {"result", result, 65536}} {
		if err := validateText(field.name, field.value, field.max); err != nil {
			return Message{}, err
		}
	}
	own, exists := s.state.Sessions[session]
	if !exists || own.Project != project || own.AgentID == "" {
		return Message{}, errors.New("stable session not found")
	}
	for i := range s.state.Messages {
		request := &s.state.Messages[i]
		if request.ID != messageID || request.Kind != "request" || !s.messageVisible(*request, own) || !addressed(*request, own) {
			continue
		}
		if request.Status == "completed" && request.AcceptedBy == session && request.AcceptanceToken == token {
			for _, message := range s.state.Messages {
				if message.ID == request.ResultMessageID {
					if message.Body != result {
						return Message{}, errors.New("request completion was already recorded with a different result")
					}
					return cloneMessage(message), nil
				}
			}
			return Message{}, errors.New("completed request result is missing")
		}
		if !s.validSession(project, session) || !s.acceptanceLive(*request, s.now().UTC()) || request.AcceptedBy != session || request.AcceptanceToken == "" || request.AcceptanceToken != token {
			return Message{}, errors.New("request completion rejected: accepting lease or fencing token is stale")
		}
		if err := s.makeMessageRoom(project); err != nil {
			return Message{}, err
		}
		request = nil
		for j := range s.state.Messages {
			if s.state.Messages[j].ID == messageID {
				request = &s.state.Messages[j]
				break
			}
		}
		if request == nil {
			return Message{}, errors.New("request was evicted during completion")
		}
		owner := s.state.Sessions[session]
		reply := Message{
			ID: id(), Project: project, Scope: request.Scope, From: session, Thread: request.Thread, Body: result,
			Created: s.now().UTC(), FromAgentID: owner.AgentID, ToAgentID: request.FromAgentID,
			ToProjectID: request.Project, SpaceID: request.SpaceID, Kind: "result", CorrelationID: request.CorrelationID,
			AuthorizationRef: request.AuthorizationRef, Status: "pending",
		}
		request.Status, request.ResultMessageID = "completed", reply.ID
		s.state.Messages = append(s.state.Messages, reply)
		return cloneMessage(reply), s.commit()
	}
	return Message{}, errors.New("request not found")
}

func (s *Store) AcquireMany(project, session string, resources []string, ttl time.Duration) ([]Claim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validSession(project, session) {
		return nil, errors.New("active session not found")
	}
	if len(resources) == 0 || len(resources) > 100 {
		return nil, errors.New("resources must contain 1 to 100 entries")
	}
	now := s.now().UTC()
	s.cleanClaims(now)
	unique := make([]string, 0, len(resources))
	seen := map[string]bool{}
	for _, resource := range resources {
		if resource == "" || len(resource) > 1024 || !utf8.ValidString(resource) || strings.ContainsRune(resource, 0) {
			return nil, errors.New("resource must be valid text of 1 to 1024 bytes")
		}
		if !seen[resource] {
			seen[resource] = true
			unique = append(unique, resource)
		}
	}
	if ttl <= 0 || ttl > 24*time.Hour {
		ttl = 30 * time.Minute
	}
	projectClaims := 0
	for _, claim := range s.state.Claims {
		if claim.Project == project {
			projectClaims++
		}
	}
	newClaims := 0
	for _, resource := range unique {
		if claim, exists := s.state.Claims[claimKey(project, resource)]; exists && claim.Session != session {
			return nil, fmt.Errorf("resource %q claimed by %s", resource, claim.Session)
		} else if !exists {
			newClaims++
		}
	}
	if projectClaims+newClaims > maxProjectClaims {
		return nil, errors.New("project claim quota exceeded")
	}
	out := make([]Claim, 0, len(unique))
	for _, resource := range unique {
		claim := Claim{Resource: resource, Project: project, Session: session, Expires: now.Add(ttl)}
		s.state.Claims[claimKey(project, resource)] = claim
		out = append(out, claim)
	}
	return out, s.commit()
}
