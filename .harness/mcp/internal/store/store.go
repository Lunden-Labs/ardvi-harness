package store

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

type Session struct {
	ID              string    `json:"id"`
	Project         string    `json:"project"`
	Name            string    `json:"name"`
	Client          string    `json:"client"`
	Task            string    `json:"task,omitempty"`
	State           string    `json:"state"`
	Updated         time.Time `json:"updated"`
	AgentID         string    `json:"agent_id,omitempty"`
	MachineID       string    `json:"machine_id,omitempty"`
	NativeSessionID string    `json:"native_session_id,omitempty"`
	NativeThreadID  string    `json:"native_thread_id,omitempty"`
	ProjectName     string    `json:"project_name,omitempty"`
	Branch          string    `json:"branch,omitempty"`
	Head            string    `json:"head,omitempty"`
	Dirty           bool      `json:"dirty,omitempty"`
	LeaseExpires    time.Time `json:"lease_expires,omitempty"`
}
type Message struct {
	ID                string    `json:"id"`
	Project           string    `json:"project"`
	Scope             string    `json:"scope"`
	From              string    `json:"from"`
	To                string    `json:"to,omitempty"`
	Thread            string    `json:"thread_id"`
	Body              string    `json:"body"`
	AckRequired       bool      `json:"ack_required,omitempty"`
	Acked             []string  `json:"acked_by,omitempty"`
	Created           time.Time `json:"created"`
	FromAgentID       string    `json:"from_agent_id,omitempty"`
	ToAgentID         string    `json:"to_agent_id,omitempty"`
	ToProjectID       string    `json:"to_project_id,omitempty"`
	ToSessionID       string    `json:"to_session_id,omitempty"`
	SpaceID           string    `json:"space_id,omitempty"`
	Kind              string    `json:"kind,omitempty"`
	CorrelationID     string    `json:"correlation_id,omitempty"`
	IdempotencyKey    string    `json:"idempotency_key,omitempty"`
	AuthorizationRef  string    `json:"authorization_ref,omitempty"`
	Status            string    `json:"status,omitempty"`
	AcceptedBy        string    `json:"accepted_by,omitempty"`
	AcceptanceExpires time.Time `json:"acceptance_expires,omitempty"`
	AcceptanceToken   string    `json:"acceptance_token,omitempty"`
	ResultMessageID   string    `json:"result_message_id,omitempty"`
}
type Claim struct {
	Resource string    `json:"resource"`
	Project  string    `json:"project"`
	Session  string    `json:"session_id"`
	Expires  time.Time `json:"expires"`
}
type Memory struct {
	ID       string    `json:"id"`
	Project  string    `json:"project"`
	Scope    string    `json:"scope"`
	Text     string    `json:"text"`
	Tags     []string  `json:"tags,omitempty"`
	Durable  bool      `json:"durable,omitempty"`
	Archived bool      `json:"archived,omitempty"`
	Updated  time.Time `json:"updated"`
}
type State struct {
	Sessions   map[string]Session     `json:"sessions"`
	Messages   []Message              `json:"messages"`
	Claims     map[string]Claim       `json:"claims"`
	Memories   map[string]Memory      `json:"memories"`
	Agents     map[string]agentRecord `json:"agents,omitempty"`
	Projects   map[string]Project     `json:"projects,omitempty"`
	GlobalDeny map[string]bool        `json:"global_deny,omitempty"`
}

type Store struct {
	mu    sync.Mutex
	path  string
	lock  *os.File
	state State
	now   func() time.Time
}

const (
	maxProjectSessions = 500
	maxProjectMessages = 1000
	maxProjectMemories = 500
	maxProjectAgents   = 500
	maxProjectClaims   = 500
)

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(dir, "writer.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, fmt.Errorf("store already in use: %w", err)
	}
	s := &Store{path: filepath.Join(dir, "state.json"), lock: lock, now: time.Now}
	if err := s.load(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock == nil {
		return nil
	}
	_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
	err := s.lock.Close()
	s.lock = nil
	return err
}

func emptyState() State {
	return State{Sessions: map[string]Session{}, Claims: map[string]Claim{}, Memories: map[string]Memory{}, Agents: map[string]agentRecord{}, Projects: map[string]Project{}, GlobalDeny: map[string]bool{}}
}

func (s *Store) load() error {
	s.state = emptyState()
	info, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() > 64<<20 {
		return errors.New("state file exceeds 64 MiB")
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &s.state); err != nil {
		return fmt.Errorf("corrupt state file: %w", err)
	}
	if s.state.Sessions == nil {
		s.state.Sessions = map[string]Session{}
	}
	if s.state.Claims == nil {
		s.state.Claims = map[string]Claim{}
	}
	if s.state.Memories == nil {
		s.state.Memories = map[string]Memory{}
	}
	if s.state.Agents == nil {
		s.state.Agents = map[string]agentRecord{}
	}
	if s.state.Projects == nil {
		s.state.Projects = map[string]Project{}
	}
	if s.state.GlobalDeny == nil {
		s.state.GlobalDeny = map[string]bool{}
	}
	return nil
}

func (s *Store) save() error {
	// ponytail: one atomic snapshot is enough for low-volume coordination state.
	b, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	if len(b) > 64<<20 {
		return errors.New("state exceeds 64 MiB")
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".state.*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(b)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, s.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (s *Store) commit() error {
	if err := s.save(); err != nil {
		_ = s.load()
		return err
	}
	return nil
}

func id() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func clamp(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}
func claimKey(project, resource string) string { return project + "\x00" + resource }

func (s *Store) StartSession(project, name, client, task string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	oldest := ""
	for id, session := range s.state.Sessions {
		if session.Project != project {
			continue
		}
		count++
		if session.State == "ended" && (oldest == "" || session.Updated.Before(s.state.Sessions[oldest].Updated)) {
			oldest = id
		}
	}
	if count >= maxProjectSessions {
		if oldest == "" {
			return Session{}, errors.New("project session quota exceeded")
		}
		delete(s.state.Sessions, oldest)
	}
	now := s.now().UTC()
	v := Session{ID: id(), Project: project, Name: name, Client: client, Task: task, State: "running", Updated: now}
	s.state.Sessions[v.ID] = v
	return v, s.commit()
}
func (s *Store) EndSession(project, session string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.state.Sessions[session]
	if !ok || v.Project != project {
		return errors.New("session not found")
	}
	v.State = "ended"
	v.Updated = s.now().UTC()
	s.state.Sessions[session] = v
	for key, claim := range s.state.Claims {
		if claim.Project == project && claim.Session == session {
			delete(s.state.Claims, key)
		}
	}
	for i := range s.state.Messages {
		message := &s.state.Messages[i]
		if message.Status == "accepted" && message.AcceptedBy == session {
			message.Status, message.AcceptedBy, message.AcceptanceToken = "pending", "", ""
			message.AcceptanceExpires = time.Time{}
		}
	}
	return s.commit()
}
func (s *Store) Sessions(project string, limit int) []Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Session{}
	for _, v := range s.state.Sessions {
		if v.Project == project {
			if v.AgentID != "" && v.State == "running" && !v.LeaseExpires.After(s.now().UTC()) {
				v.State = "stale"
			}
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	if len(out) > clamp(limit) {
		out = out[:clamp(limit)]
	}
	return out
}
func (s *Store) validSession(project, session string) bool {
	v, ok := s.state.Sessions[session]
	return ok && v.Project == project && v.State == "running" && (v.AgentID == "" || v.LeaseExpires.After(s.now().UTC()))
}
func addressed(m Message, session Session) bool {
	if m.ToSessionID != "" {
		return m.ToSessionID == session.ID
	}
	if m.ToAgentID != "" {
		return session.AgentID != "" && m.ToAgentID == session.AgentID
	}
	if m.ToProjectID != "" {
		return m.ToProjectID == session.Project
	}
	return m.To == "" || m.To == "*" || m.To == session.ID || m.To == session.Name
}
func ackIdentity(session Session) string {
	if session.AgentID != "" {
		return session.AgentID
	}
	return session.ID
}
func acknowledged(m Message, session Session) bool {
	identity := ackIdentity(session)
	for _, id := range m.Acked {
		if id == identity {
			return true
		}
	}
	return false
}

func (s *Store) Send(project, scope, from, to, thread, body string, ack bool) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validSession(project, from) {
		return Message{}, errors.New("active session not found")
	}
	if scope == "" {
		scope = "project"
	}
	if scope != "project" && scope != "global" {
		return Message{}, errors.New("scope must be project or global")
	}
	if scope == "global" && !s.globalAllowed(project) {
		return Message{}, errors.New("global access denied for project")
	}
	if err := s.makeMessageRoom(project); err != nil {
		return Message{}, err
	}
	v := Message{ID: id(), Project: project, Scope: scope, From: from, To: to, Thread: thread, Body: body, AckRequired: ack, Created: s.now().UTC(), Status: "sent"}
	if ack {
		v.Status = "pending"
	}
	if v.Thread == "" {
		v.Thread = v.ID
	}
	s.state.Messages = append(s.state.Messages, v)
	return v, s.commit()
}
func (s *Store) Inbox(project, session string, limit int) ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	own, ok := s.state.Sessions[session]
	if !ok || !s.validSession(project, session) {
		return nil, errors.New("active session not found")
	}
	out := []Message{}
	for i := len(s.state.Messages) - 1; i >= 0 && len(out) < clamp(limit); i-- {
		m := s.state.Messages[i]
		if s.messageVisible(m, own) && addressed(m, own) && !acknowledged(m, own) {
			out = append(out, cloneMessage(m))
		}
	}
	return out, nil
}
func (s *Store) Ack(project, session, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validSession(project, session) {
		return errors.New("active session not found")
	}
	for i := range s.state.Messages {
		m := &s.state.Messages[i]
		if m.ID == message && s.messageVisible(*m, s.state.Sessions[session]) {
			owner := s.state.Sessions[session]
			if !addressed(*m, owner) {
				return errors.New("message is not addressed to this session")
			}
			identity := ackIdentity(owner)
			for _, a := range m.Acked {
				if a == identity {
					return nil
				}
			}
			m.Acked = append(m.Acked, identity)
			if m.Kind != "request" && (m.ToAgentID != "" || m.ToSessionID != "") {
				m.Status = "acknowledged"
			}
			return s.commit()
		}
	}
	return errors.New("message not found")
}

// UnreadCount returns the total number of messages visible to session that
// are not yet acknowledged by it, unbounded by any page limit.
func (s *Store) UnreadCount(project, session string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	own, ok := s.state.Sessions[session]
	if !ok || !s.validSession(project, session) {
		return 0
	}
	count := 0
	for _, m := range s.state.Messages {
		if s.messageVisible(m, own) && addressed(m, own) && !acknowledged(m, own) {
			count++
		}
	}
	return count
}
func (s *Store) Thread(project, thread string, limit int) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Message{}
	for _, m := range s.state.Messages {
		if m.Thread == thread && s.messageVisibleToProject(m, project) {
			out = append(out, cloneMessage(m))
		}
	}
	if len(out) > clamp(limit) {
		out = out[len(out)-clamp(limit):]
	}
	return out
}

func (s *Store) cleanClaims(now time.Time) {
	for k, c := range s.state.Claims {
		owner, exists := s.state.Sessions[c.Session]
		if !c.Expires.After(now) || !exists || owner.AgentID != "" && !s.validSession(c.Project, c.Session) {
			delete(s.state.Claims, k)
		}
	}
}
func (s *Store) Acquire(project, session, resource string, ttl time.Duration) (Claim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.validSession(project, session) {
		return Claim{}, errors.New("active session not found")
	}
	if resource == "" || len(resource) > 1024 || !utf8.ValidString(resource) || strings.ContainsRune(resource, 0) {
		return Claim{}, errors.New("resource must be valid text of 1 to 1024 bytes")
	}
	now := s.now().UTC()
	s.cleanClaims(now)
	key := claimKey(project, resource)
	if c, ok := s.state.Claims[key]; ok && c.Session != session {
		return Claim{}, fmt.Errorf("resource claimed by %s", c.Session)
	}
	if _, exists := s.state.Claims[key]; !exists {
		count := 0
		for _, claim := range s.state.Claims {
			if claim.Project == project {
				count++
			}
		}
		if count >= maxProjectClaims {
			return Claim{}, errors.New("project claim quota exceeded")
		}
	}
	if ttl <= 0 || ttl > 24*time.Hour {
		ttl = 30 * time.Minute
	}
	c := Claim{Resource: resource, Project: project, Session: session, Expires: now.Add(ttl)}
	s.state.Claims[key] = c
	return c, s.commit()
}
func (s *Store) Release(project, session, resource string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := claimKey(project, resource)
	c, ok := s.state.Claims[key]
	if !ok {
		return nil
	}
	if c.Session != session {
		return errors.New("claim owned by another session")
	}
	delete(s.state.Claims, key)
	return s.commit()
}
func (s *Store) Claims(project string) []Claim {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanClaims(s.now().UTC())
	out := []Claim{}
	for _, c := range s.state.Claims {
		if c.Project == project {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Resource < out[j].Resource })
	if len(out) > 100 {
		out = out[:100]
	}
	return out
}

func (s *Store) PutMemory(project, scope, text string, tags []string, durable bool) (Memory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if scope == "" {
		scope = "project"
	}
	if scope != "project" && scope != "global" {
		return Memory{}, errors.New("scope must be project or global")
	}
	if len(text) == 0 || len(text) > 65536 || !utf8.ValidString(text) || strings.ContainsRune(text, 0) || len(tags) > 20 {
		return Memory{}, errors.New("memory text or tags exceed limits")
	}
	for _, tag := range tags {
		if len(tag) > 120 || !utf8.ValidString(tag) || strings.ContainsRune(tag, 0) {
			return Memory{}, errors.New("memory text or tags exceed limits")
		}
	}
	if scope == "global" && !s.globalAllowed(project) {
		return Memory{}, errors.New("global access denied for project")
	}
	count := 0
	oldest := ""
	for id, memory := range s.state.Memories {
		if memory.Project != project {
			continue
		}
		count++
		if (memory.Archived || !memory.Durable) && (oldest == "" || memory.Updated.Before(s.state.Memories[oldest].Updated)) {
			oldest = id
		}
	}
	if count >= maxProjectMemories {
		if oldest == "" {
			return Memory{}, errors.New("project memory quota exceeded; archive or export durable memory")
		}
		delete(s.state.Memories, oldest)
	}
	m := Memory{ID: id(), Project: project, Scope: scope, Text: text, Tags: append([]string(nil), tags...), Durable: durable, Updated: s.now().UTC()}
	s.state.Memories[m.ID] = m
	return cloneMemory(m), s.commit()
}
func (s *Store) SearchMemory(project, query, scope string, limit int) []Memory {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := strings.ToLower(query)
	out := []Memory{}
	for _, m := range s.state.Memories {
		if m.Archived {
			continue
		}
		visible := m.Project == project && m.Scope == "project"
		if scope == "global" {
			visible = m.Scope == "global" && s.globalAllowed(project) && s.globalAllowed(m.Project)
		} else if scope == "" {
			visible = visible || m.Scope == "global" && s.globalAllowed(project) && s.globalAllowed(m.Project)
		}
		if !visible {
			continue
		}
		hay := strings.ToLower(m.Text + " " + strings.Join(m.Tags, " "))
		if q == "" || strings.Contains(hay, q) {
			out = append(out, cloneMemory(m))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	if len(out) > clamp(limit) {
		out = out[:clamp(limit)]
	}
	return out
}
func (s *Store) ArchiveMemory(project, id, scope string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if scope == "" {
		scope = "project"
	}
	if scope != "project" && scope != "global" {
		return errors.New("scope must be project or global")
	}
	m, ok := s.state.Memories[id]
	if !ok || m.Project != project || m.Scope != scope {
		return errors.New("memory not found")
	}
	m.Archived = true
	m.Updated = s.now().UTC()
	s.state.Memories[id] = m
	return s.commit()
}
func (s *Store) ExportMemory(project string) []Memory {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Memory{}
	for _, m := range s.state.Memories {
		if m.Project == project && m.Scope == "project" && m.Durable && !m.Archived {
			out = append(out, cloneMemory(m))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.Before(out[j].Updated) })
	return out
}
func (s *Store) ImportMemory(project string, items []Memory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(items) > maxProjectMemories {
		return fmt.Errorf("memory import exceeds %d items", maxProjectMemories)
	}
	existing := 0
	projectIDs := map[string]bool{}
	for _, memory := range s.state.Memories {
		if memory.Project == project {
			existing++
			if memory.Scope == "project" {
				projectIDs[memory.ID] = true
			}
		}
	}
	added := 0
	for _, memory := range items {
		if memory.ID == "" || !projectIDs[memory.ID] {
			added++
		}
	}
	if existing+added > maxProjectMemories {
		return errors.New("project memory quota exceeded")
	}
	for _, m := range items {
		if len(m.Text) == 0 || len(m.Text) > 65536 || !utf8.ValidString(m.Text) || strings.ContainsRune(m.Text, 0) || len(m.Tags) > 20 {
			return errors.New("invalid memory import item")
		}
		for _, tag := range m.Tags {
			if len(tag) > 120 || !utf8.ValidString(tag) || strings.ContainsRune(tag, 0) {
				return errors.New("invalid memory import item")
			}
		}
	}
	for _, m := range items {
		m.Tags = append([]string(nil), m.Tags...)
		m.Project = project
		m.Scope = "project"
		if m.ID == "" {
			m.ID = id()
		} else if existing, ok := s.state.Memories[m.ID]; ok && (existing.Project != project || existing.Scope != "project") {
			m.ID = id()
		}
		if m.Updated.IsZero() {
			m.Updated = s.now().UTC()
		}
		s.state.Memories[m.ID] = m
	}
	return s.commit()
}

func cloneMemory(memory Memory) Memory {
	memory.Tags = append([]string(nil), memory.Tags...)
	return memory
}

func ReadExport(path string) ([]Memory, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return ReadExportFrom(f)
}
func ReadExportFrom(reader io.Reader) ([]Memory, error) {
	out := []Memory{}
	scan := bufio.NewScanner(reader)
	scan.Buffer(make([]byte, 64*1024), 1024*1024)
	total := 0
	for scan.Scan() {
		if len(out) >= 10000 {
			return nil, errors.New("memory import exceeds 10000 items")
		}
		total += len(scan.Bytes())
		if total > 64<<20 {
			return nil, errors.New("memory import exceeds 64 MiB")
		}
		var m Memory
		if err := json.Unmarshal(scan.Bytes(), &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, scan.Err()
}
func WriteExport(path string, items []Memory) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if e = WriteExportTo(f, items); e != nil {
		f.Close()
		return e
	}
	return f.Close()
}
func WriteExportTo(writer io.Writer, items []Memory) error {
	enc := json.NewEncoder(writer)
	for _, m := range items {
		if err := enc.Encode(m); err != nil {
			return err
		}
	}
	return nil
}
