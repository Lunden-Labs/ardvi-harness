package store

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Session struct {
	ID      string    `json:"id"`
	Project string    `json:"project"`
	Name    string    `json:"name"`
	Client  string    `json:"client"`
	Task    string    `json:"task,omitempty"`
	State   string    `json:"state"`
	Updated time.Time `json:"updated"`
}
type Message struct {
	ID          string    `json:"id"`
	Project     string    `json:"project"`
	Scope       string    `json:"scope"`
	From        string    `json:"from"`
	To          string    `json:"to,omitempty"`
	Thread      string    `json:"thread_id"`
	Body        string    `json:"body"`
	AckRequired bool      `json:"ack_required,omitempty"`
	Acked       []string  `json:"acked_by,omitempty"`
	Created     time.Time `json:"created"`
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
	Sessions map[string]Session `json:"sessions"`
	Messages []Message          `json:"messages"`
	Claims   map[string]Claim   `json:"claims"`
	Memories map[string]Memory  `json:"memories"`
}

type Store struct {
	mu    sync.Mutex
	path  string
	lock  *os.File
	state State
	now   func() time.Time
}

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
	return State{Sessions: map[string]Session{}, Claims: map[string]Claim{}, Memories: map[string]Memory{}}
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
	return s.commit()
}
func (s *Store) Sessions(project string, limit int) []Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Session{}
	for _, v := range s.state.Sessions {
		if v.Project == project {
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
	return ok && v.Project == project && v.State == "running"
}
func addressed(m Message, session, name string) bool {
	return m.To == "" || m.To == "*" || m.To == session || m.To == name
}
func acknowledged(m Message, session string) bool {
	for _, id := range m.Acked {
		if id == session {
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
	v := Message{ID: id(), Project: project, Scope: scope, From: from, To: to, Thread: thread, Body: body, AckRequired: ack, Created: s.now().UTC()}
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
	if !ok || own.Project != project {
		return nil, errors.New("session not found")
	}
	out := []Message{}
	for i := len(s.state.Messages) - 1; i >= 0 && len(out) < clamp(limit); i-- {
		m := s.state.Messages[i]
		if (m.Scope == "global" || m.Project == project) && addressed(m, session, own.Name) && !acknowledged(m, session) {
			out = append(out, m)
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
		if m.ID == message && (m.Scope == "global" || m.Project == project) {
			owner := s.state.Sessions[session]
			if !addressed(*m, session, owner.Name) {
				return errors.New("message is not addressed to this session")
			}
			for _, a := range m.Acked {
				if a == session {
					return nil
				}
			}
			m.Acked = append(m.Acked, session)
			return s.commit()
		}
	}
	return errors.New("message not found")
}
func (s *Store) Thread(project, thread string, limit int) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Message{}
	for _, m := range s.state.Messages {
		if m.Thread == thread && (m.Scope == "global" || m.Project == project) {
			out = append(out, m)
		}
	}
	if len(out) > clamp(limit) {
		out = out[len(out)-clamp(limit):]
	}
	return out
}

func (s *Store) cleanClaims(now time.Time) {
	for k, c := range s.state.Claims {
		if !c.Expires.After(now) {
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
	now := s.now().UTC()
	s.cleanClaims(now)
	key := claimKey(project, resource)
	if c, ok := s.state.Claims[key]; ok && c.Session != session {
		return Claim{}, fmt.Errorf("resource claimed by %s", c.Session)
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
	m := Memory{ID: id(), Project: project, Scope: scope, Text: text, Tags: tags, Durable: durable, Updated: s.now().UTC()}
	s.state.Memories[m.ID] = m
	return m, s.commit()
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
		if scope == "global" && m.Scope != "global" {
			continue
		}
		if scope != "global" && !(m.Scope == "global" || m.Project == project) {
			continue
		}
		hay := strings.ToLower(m.Text + " " + strings.Join(m.Tags, " "))
		if q == "" || strings.Contains(hay, q) {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	if len(out) > clamp(limit) {
		out = out[:clamp(limit)]
	}
	return out
}
func (s *Store) ArchiveMemory(project, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.state.Memories[id]
	if !ok || (m.Scope == "project" && m.Project != project) {
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
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.Before(out[j].Updated) })
	return out
}
func (s *Store) ImportMemory(project string, items []Memory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(items) > 10000 {
		return errors.New("memory import exceeds 10000 items")
	}
	for _, m := range items {
		if len(m.Text) == 0 || len(m.Text) > 65536 || len(m.Tags) > 20 {
			return errors.New("invalid memory import item")
		}
	}
	for _, m := range items {
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

func ReadExport(path string) ([]Memory, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	out := []Memory{}
	scan := bufio.NewScanner(f)
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
		if e = json.Unmarshal(scan.Bytes(), &m); e != nil {
			return nil, e
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
	enc := json.NewEncoder(f)
	for _, m := range items {
		if e = enc.Encode(m); e != nil {
			f.Close()
			return e
		}
	}
	return f.Close()
}
