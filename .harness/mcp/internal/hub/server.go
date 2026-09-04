package hub

import (
	"context"
	"encoding/base64"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ardvi/harness/mcp/internal/catalog"
	"github.com/ardvi/harness/mcp/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var projectPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89aAbB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func project(req *mcp.CallToolRequest) (string, error) {
	if req.Extra == nil {
		return "", errors.New("missing X-Ardvi-Project header")
	}
	v := req.Extra.Header.Get("X-Ardvi-Project")
	if !projectPattern.MatchString(v) {
		return "", errors.New("invalid X-Ardvi-Project header")
	}
	return v, nil
}
func ro(name, description string) *mcp.Tool {
	return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}
}
func rw(name, description string) *mcp.Tool {
	destructive := false
	return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &destructive}}
}
const maxTextBytes = 16 * 1024 // one message or memory item should stay small next to the store's 64 MiB snapshot cap.

func bounded(value, label string, max int) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New(label + " is required")
	}
	if len(value) > max {
		return errors.New(label + " is too long")
	}
	return nil
}

type empty struct{}
type sessionStartIn struct {
	Name   string `json:"name"`
	Client string `json:"client"`
	Task   string `json:"task,omitempty"`
}
type sessionIn struct {
	SessionID string `json:"session_id"`
}
type listIn struct {
	Limit int `json:"limit,omitempty"`
}
type sessionsOut struct {
	Sessions []store.Session `json:"sessions"`
}
type messageSendIn struct {
	SessionID   string `json:"session_id"`
	To          string `json:"to,omitempty"`
	ThreadID    string `json:"thread_id,omitempty"`
	Body        string `json:"body"`
	Scope       string `json:"scope,omitempty"`
	AckRequired bool   `json:"ack_required,omitempty"`
}
type inboxIn struct {
	SessionID string `json:"session_id"`
	Limit     int    `json:"limit,omitempty"`
}
type ackIn struct {
	SessionID string `json:"session_id"`
	MessageID string `json:"message_id"`
}
type threadIn struct {
	ThreadID string `json:"thread_id"`
	Limit    int    `json:"limit,omitempty"`
}
type messagesOut struct {
	Messages []store.Message `json:"messages"`
}

// withUnread piggybacks a bounded preview of a session's unread inbox onto
// tool outputs the session is likely to poll right after acting, so a client
// need not make a separate inbox_read round trip on every turn.
type withUnread struct {
	Unread      []store.Message `json:"unread,omitempty"`
	UnreadCount int             `json:"unread_count,omitempty"`
}
type messageSendOut struct {
	store.Message
	withUnread
}
type claimAcquireOut struct {
	store.Claim
	withUnread
}
type unreadOut struct {
	withUnread
}
type claimIn struct {
	SessionID  string `json:"session_id"`
	Resource   string `json:"resource"`
	TTLMinutes int    `json:"ttl_minutes,omitempty"`
}
type claimsOut struct {
	Claims []store.Claim `json:"claims"`
}
type memoryPutIn struct {
	Text    string   `json:"text"`
	Tags    []string `json:"tags,omitempty"`
	Scope   string   `json:"scope,omitempty"`
	Durable bool     `json:"durable,omitempty"`
}
type memorySearchIn struct {
	Query string `json:"query,omitempty"`
	Scope string `json:"scope,omitempty"`
	Limit int    `json:"limit,omitempty"`
}
type memoryArchiveIn struct {
	ID    string `json:"id"`
	Scope string `json:"scope,omitempty"`
}
type memoriesOut struct {
	Memories []store.Memory `json:"memories"`
}
type searchIn struct {
	Query string `json:"query,omitempty"`
	Limit int    `json:"limit,omitempty"`
}
type catalogOut struct {
	Entries []catalog.Entry `json:"entries"`
}
type skillSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
}
type skillsListOut struct {
	Skills    []skillSummary    `json:"skills"`
	Revisions map[string]string `json:"revisions"`
	Next      string            `json:"next_cursor,omitempty"`
}
type skillsListIn struct {
	Source string `json:"source,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}
type skillReadIn struct {
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
}
type personaReadIn struct {
	Name string `json:"name"`
}
type contentOut struct {
	Entry   catalog.Entry   `json:"entry"`
	Content catalog.Content `json:"content"`
}

// unread returns a capped preview (at most 10) of session's unacknowledged
// inbox plus the true total count, or the zero value if session has none —
// so the field is simply absent from a tool's JSON output.
func unread(s *store.Store, project, session string) withUnread {
	if session == "" {
		return withUnread{}
	}
	messages, err := s.Inbox(project, session, 10)
	if err != nil || len(messages) == 0 {
		return withUnread{}
	}
	return withUnread{Unread: messages, UnreadCount: s.UnreadCount(project, session)}
}

func New(s *store.Store, c *catalog.Catalog, version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "ardvi-mcp", Version: version}, nil)
	mcp.AddTool(server, rw("session_start", "Register this native Codex or Claude session."), func(ctx context.Context, req *mcp.CallToolRequest, in sessionStartIn) (*mcp.CallToolResult, store.Session, error) {
		p, e := project(req)
		if e != nil {
			return nil, store.Session{}, e
		}
		if e = bounded(in.Name, "name", 120); e != nil {
			return nil, store.Session{}, e
		}
		if e = bounded(in.Client, "client", 80); e != nil {
			return nil, store.Session{}, e
		}
		if len(in.Task) > 4000 {
			return nil, store.Session{}, errors.New("task is too long")
		}
		v, e := s.StartSession(p, in.Name, in.Client, in.Task)
		return nil, v, e
	})
	mcp.AddTool(server, rw("session_end", "End a session and release all of its live claims."), func(ctx context.Context, req *mcp.CallToolRequest, in sessionIn) (*mcp.CallToolResult, empty, error) {
		p, e := project(req)
		if e == nil {
			e = s.EndSession(p, in.SessionID)
		}
		return nil, empty{}, e
	})
	mcp.AddTool(server, ro("agents_list", "List recent native sessions in this project."), func(ctx context.Context, req *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, sessionsOut, error) {
		p, e := project(req)
		if e != nil {
			return nil, sessionsOut{}, e
		}
		return nil, sessionsOut{s.Sessions(p, in.Limit)}, nil
	})

	mcp.AddTool(server, rw("message_send", "Send a project message, or an explicit global message across projects."), func(ctx context.Context, req *mcp.CallToolRequest, in messageSendIn) (*mcp.CallToolResult, messageSendOut, error) {
		p, e := project(req)
		if e != nil {
			return nil, messageSendOut{}, e
		}
		if e = bounded(in.Body, "body", maxTextBytes); e != nil {
			return nil, messageSendOut{}, e
		}
		if len(in.To) > 120 || len(in.ThreadID) > 120 {
			return nil, messageSendOut{}, errors.New("recipient or thread_id is too long")
		}
		v, e := s.Send(p, in.Scope, in.SessionID, in.To, in.ThreadID, in.Body, in.AckRequired)
		if e != nil {
			return nil, messageSendOut{}, e
		}
		return nil, messageSendOut{Message: v, withUnread: unread(s, p, in.SessionID)}, nil
	})
	mcp.AddTool(server, ro("inbox_read", "Read messages addressed to a session or its registered name."), func(ctx context.Context, req *mcp.CallToolRequest, in inboxIn) (*mcp.CallToolResult, messagesOut, error) {
		p, e := project(req)
		if e != nil {
			return nil, messagesOut{}, e
		}
		v, e := s.Inbox(p, in.SessionID, in.Limit)
		return nil, messagesOut{v}, e
	})
	mcp.AddTool(server, rw("message_ack", "Acknowledge receipt of a message."), func(ctx context.Context, req *mcp.CallToolRequest, in ackIn) (*mcp.CallToolResult, unreadOut, error) {
		p, e := project(req)
		if e == nil {
			e = s.Ack(p, in.SessionID, in.MessageID)
		}
		if e != nil {
			return nil, unreadOut{}, e
		}
		return nil, unreadOut{withUnread: unread(s, p, in.SessionID)}, nil
	})
	mcp.AddTool(server, ro("thread_read", "Read a bounded message thread visible to this project."), func(ctx context.Context, req *mcp.CallToolRequest, in threadIn) (*mcp.CallToolResult, messagesOut, error) {
		p, e := project(req)
		if e != nil {
			return nil, messagesOut{}, e
		}
		return nil, messagesOut{s.Thread(p, in.ThreadID, in.Limit)}, nil
	})

	mcp.AddTool(server, ro("claims_list", "List unexpired file or resource claims in this project."), func(ctx context.Context, req *mcp.CallToolRequest, in empty) (*mcp.CallToolResult, claimsOut, error) {
		p, e := project(req)
		if e != nil {
			return nil, claimsOut{}, e
		}
		return nil, claimsOut{s.Claims(p)}, nil
	})
	mcp.AddTool(server, rw("claim_acquire", "Atomically acquire or renew a project resource claim."), func(ctx context.Context, req *mcp.CallToolRequest, in claimIn) (*mcp.CallToolResult, claimAcquireOut, error) {
		p, e := project(req)
		if e != nil {
			return nil, claimAcquireOut{}, e
		}
		if e = bounded(in.Resource, "resource", 1024); e != nil {
			return nil, claimAcquireOut{}, e
		}
		v, e := s.Acquire(p, in.SessionID, in.Resource, time.Duration(in.TTLMinutes)*time.Minute)
		if e != nil {
			return nil, claimAcquireOut{}, e
		}
		return nil, claimAcquireOut{Claim: v, withUnread: unread(s, p, in.SessionID)}, nil
	})
	mcp.AddTool(server, rw("claim_release", "Release a project resource claim owned by this session."), func(ctx context.Context, req *mcp.CallToolRequest, in claimIn) (*mcp.CallToolResult, unreadOut, error) {
		p, e := project(req)
		if e == nil {
			e = s.Release(p, in.SessionID, in.Resource)
		}
		if e != nil {
			return nil, unreadOut{}, e
		}
		return nil, unreadOut{withUnread: unread(s, p, in.SessionID)}, nil
	})

	mcp.AddTool(server, rw("memory_put", "Store a concise project or explicit global memory item."), func(ctx context.Context, req *mcp.CallToolRequest, in memoryPutIn) (*mcp.CallToolResult, store.Memory, error) {
		p, e := project(req)
		if e != nil {
			return nil, store.Memory{}, e
		}
		if e = bounded(in.Text, "text", maxTextBytes); e != nil {
			return nil, store.Memory{}, e
		}
		if len(in.Tags) > 20 {
			return nil, store.Memory{}, errors.New("too many tags")
		}
		v, e := s.PutMemory(p, in.Scope, in.Text, in.Tags, in.Durable)
		return nil, v, e
	})
	mcp.AddTool(server, ro("memory_search", "Search visible project and global memory."), func(ctx context.Context, req *mcp.CallToolRequest, in memorySearchIn) (*mcp.CallToolResult, memoriesOut, error) {
		p, e := project(req)
		if e != nil {
			return nil, memoriesOut{}, e
		}
		if len(in.Query) > 4096 {
			return nil, memoriesOut{}, errors.New("query is too long")
		}
		if in.Scope != "" && in.Scope != "project" && in.Scope != "global" {
			return nil, memoriesOut{}, errors.New("scope must be project or global")
		}
		return nil, memoriesOut{s.SearchMemory(p, in.Query, in.Scope, in.Limit)}, nil
	})
	mcp.AddTool(server, rw("memory_archive", "Archive a visible memory item without deleting history."), func(ctx context.Context, req *mcp.CallToolRequest, in memoryArchiveIn) (*mcp.CallToolResult, empty, error) {
		p, e := project(req)
		if e == nil {
			e = s.ArchiveMemory(p, in.ID, in.Scope)
		}
		return nil, empty{}, e
	})

	mcp.AddTool(server, ro("skills_search", "Search the installed skill catalog without loading skill bodies."), func(ctx context.Context, req *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, catalogOut, error) {
		if _, e := project(req); e != nil {
			return nil, catalogOut{}, e
		}
		if len(in.Query) > 4096 {
			return nil, catalogOut{}, errors.New("query is too long")
		}
		return nil, catalogOut{c.SearchSkills(in.Query, in.Limit)}, nil
	})
	mcp.AddTool(server, ro("skills_list", "List installed skills and exact managed upstream revisions. Follow next_cursor to enumerate the full catalog."), func(ctx context.Context, req *mcp.CallToolRequest, in skillsListIn) (*mcp.CallToolResult, skillsListOut, error) {
		if len(in.Source) > 120 {
			return nil, skillsListOut{}, errors.New("source is too long")
		}
		offset := 0
		if in.Cursor != "" {
			decoded, e := base64.RawURLEncoding.DecodeString(in.Cursor)
			if e != nil {
				return nil, skillsListOut{}, errors.New("invalid cursor")
			}
			offset, e = strconv.Atoi(string(decoded))
			if e != nil || offset < 0 {
				return nil, skillsListOut{}, errors.New("invalid cursor")
			}
		}
		entries := c.ListSkills()
		filtered := entries[:0]
		for _, entry := range entries {
			if in.Source == "" || entry.Source == in.Source {
				filtered = append(filtered, entry)
			}
		}
		if offset > len(filtered) {
			return nil, skillsListOut{}, errors.New("invalid cursor")
		}
		limit := in.Limit
		if limit <= 0 || limit > 100 {
			limit = 100
		}
		end := offset + limit
		if end > len(filtered) {
			end = len(filtered)
		}
		skills := make([]skillSummary, 0, end-offset)
		for _, entry := range filtered[offset:end] {
			skills = append(skills, skillSummary{ID: entry.Source + ":" + entry.Name, Name: entry.Name, Description: entry.Description, Source: entry.Source})
		}
		next := ""
		if end < len(filtered) {
			next = base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(end)))
		}
		return nil, skillsListOut{Skills: skills, Revisions: c.Revisions, Next: next}, nil
	})
	mcp.AddTool(server, ro("skill_read", "Read a skill entry point or a supporting file inside its managed checkout."), func(ctx context.Context, req *mcp.CallToolRequest, in skillReadIn) (*mcp.CallToolResult, contentOut, error) {
		if _, e := project(req); e != nil {
			return nil, contentOut{}, e
		}
		if e := bounded(in.Name, "name", 200); e != nil || len(in.Path) > 4096 {
			if e == nil {
				e = errors.New("path is too long")
			}
			return nil, contentOut{}, e
		}
		entry, text, e := c.ReadSkill(in.Name, in.Path)
		return nil, contentOut{entry, text}, e
	})
	mcp.AddTool(server, ro("personas_search", "Search optional Agency Agents personas without assigning a role."), func(ctx context.Context, req *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, catalogOut, error) {
		if _, e := project(req); e != nil {
			return nil, catalogOut{}, e
		}
		if len(in.Query) > 4096 {
			return nil, catalogOut{}, errors.New("query is too long")
		}
		return nil, catalogOut{c.SearchPersonas(in.Query, in.Limit)}, nil
	})
	mcp.AddTool(server, ro("persona_read", "Read one optional persona as guidance, not authority."), func(ctx context.Context, req *mcp.CallToolRequest, in personaReadIn) (*mcp.CallToolResult, contentOut, error) {
		if _, e := project(req); e != nil {
			return nil, contentOut{}, e
		}
		if e := bounded(in.Name, "name", 200); e != nil {
			return nil, contentOut{}, e
		}
		entry, text, e := c.ReadPersona(in.Name)
		return nil, contentOut{entry, text}, e
	})
	return server
}
