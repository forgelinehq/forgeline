package server

import (
	"context"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/docstore"
)

// Conversation history (design note 027). A conversation belongs to the person who had it. It keeps the
// questions, the answers, the names of the lookups used and the ids of what the chat filed; never a lookup
// result. Only the owner can list, read or delete it, an administrator included.

const (
	kindChat          = "chat"
	maxConversations  = 100
	maxChatMessages   = 60
	chatKeepFor       = 90 * 24 * time.Hour
	maxStoredAnswer   = 8000
	conversationTitle = 80
)

type chatMsg struct {
	Role    string    `json:"role"` // user or assistant
	Text    string    `json:"text"`
	Time    time.Time `json:"time"`
	Lookups []string  `json:"lookups,omitempty"`
}

// chatRef is something a conversation filed: a change proposal or an environment request.
type chatRef struct {
	Kind string    `json:"kind"` // proposal or environment
	ID   string    `json:"id"`
	Time time.Time `json:"time"`
}

// Conversation is a stored chat.
type Conversation struct {
	ID       string    `json:"id"`
	Owner    string    `json:"owner"` // the person's user id
	Title    string    `json:"title"`
	Created  time.Time `json:"created"`
	Updated  time.Time `json:"updated"`
	Messages []chatMsg `json:"messages"`
	Refs     []chatRef `json:"refs,omitempty"`
}

// ConversationSummaryJSON is one row of the history list.
type ConversationSummaryJSON struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Updated  time.Time `json:"updated"`
	Messages int       `json:"messages"`
	Refs     []chatRef `json:"refs,omitempty"`
}

var conversationIDRE = regexp.MustCompile(`^[0-9a-f]{32}$`)

// secretShapes are the forms a credential takes in text. Hiding them is a courtesy to people who paste a key
// by accident; it is not a boundary, and a transformed value gets through (see internal/redact).
var secretShapes = []*regexp.Regexp{
	regexp.MustCompile(`\b(?:AKIA|ASIA|AIDA|AROA)[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}\b`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`),
	regexp.MustCompile(`\bflt_[A-Za-z0-9_-]{20,}\b`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`),
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)`),
	regexp.MustCompile(`(?i)\b(?:secret|password|passwd|token|api[_-]?key|access[_-]?key)\b\s*[:=]\s*["']?[^\s"']{8,}`),
}

// hideSecrets replaces anything shaped like a credential and reports whether it changed anything.
func hideSecrets(s string) (string, bool) {
	out := s
	for _, re := range secretShapes {
		out = re.ReplaceAllString(out, "[hidden]")
	}
	return out, out != s
}

// conversationsOf returns one person's conversations, newest first.
func (s *Server) conversationsOf(ctx context.Context, userID string) ([]Conversation, error) {
	docs, err := s.docs.List(ctx, kindChat, docstore.ListOptions{})
	if err != nil {
		return nil, err
	}
	all, err := docstore.Decode[Conversation](docs)
	if err != nil {
		return nil, err
	}
	var mine []Conversation
	for _, c := range all {
		if c.Owner == userID {
			mine = append(mine, c)
		}
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].Updated.After(mine[j].Updated) })
	return mine, nil
}

func (s *Server) getConversation(ctx context.Context, userID, id string) (Conversation, bool) {
	if !conversationIDRE.MatchString(id) || userID == "" {
		return Conversation{}, false
	}
	var c Conversation
	if err := s.docs.Get(ctx, kindChat, id, &c); err != nil || c.Owner != userID {
		return Conversation{}, false // someone else's conversation is indistinguishable from none
	}
	return c, true
}

// saveTurn appends a finished question and answer to the person's conversation, starting it if needed, and
// applies the limits. A principal without a user id (the shared token) keeps no history.
func (s *Server) saveTurn(ctx context.Context, p Principal, id, question, answer string, lookups []string, refs []chatRef) {
	if p.UserID == "" || !conversationIDRE.MatchString(id) {
		return
	}
	now := time.Now().UTC()
	c, ok := s.getConversation(ctx, p.UserID, id)
	if !ok {
		var other Conversation
		if s.docs.Get(ctx, kindChat, id, &other) == nil {
			return // the id belongs to someone else: never write into it
		}
		q, _ := hideSecrets(question)
		c = Conversation{ID: id, Owner: p.UserID, Created: now, Title: truncate(strings.Join(strings.Fields(q), " "), conversationTitle)}
	}
	q, _ := hideSecrets(question)
	a, _ := hideSecrets(answer)
	c.Messages = append(c.Messages,
		chatMsg{Role: "user", Text: truncate(q, maxAssistMessage), Time: now},
		chatMsg{Role: "assistant", Text: truncate(a, maxStoredAnswer), Time: now, Lookups: lookups})
	if over := len(c.Messages) - maxChatMessages; over > 0 {
		c.Messages = c.Messages[over:]
	}
	for _, r := range refs {
		r.Time = now
		c.Refs = append(c.Refs, r)
	}
	c.Updated = now
	if err := s.docs.Put(context.WithoutCancel(ctx), kindChat, c.ID, c); err != nil {
		s.log.Error("saving a conversation", "err", err)
		return
	}
	s.trimConversations(ctx, p.UserID)
}

// trimConversations removes a person's conversations that are old or beyond the limit.
func (s *Server) trimConversations(ctx context.Context, userID string) {
	mine, err := s.conversationsOf(ctx, userID)
	if err != nil {
		return
	}
	for i, c := range mine {
		if i >= maxConversations || time.Since(c.Updated) > chatKeepFor {
			_ = s.docs.Delete(context.WithoutCancel(ctx), kindChat, c.ID)
		}
	}
}

func (s *Server) handleListConversations(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	out := []ConversationSummaryJSON{}
	if p.UserID != "" {
		mine, err := s.conversationsOf(r.Context(), p.UserID)
		if err != nil {
			s.internalError(w, "reading your conversations", err)
			return
		}
		for _, c := range mine {
			out = append(out, ConversationSummaryJSON{ID: c.ID, Title: c.Title, Updated: c.Updated, Messages: len(c.Messages), Refs: c.Refs})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetConversation(w http.ResponseWriter, r *http.Request) {
	c, ok := s.getConversation(r.Context(), principalFrom(r.Context()).UserID, r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such conversation")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleDeleteConversation(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	c, ok := s.getConversation(r.Context(), p.UserID, r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such conversation")
		return
	}
	if err := s.docs.Delete(r.Context(), kindChat, c.ID); err != nil {
		s.internalError(w, "deleting the conversation", err)
		return
	}
	s.auditReq(r, "chat.delete", "chat/"+c.ID, "", true)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDeleteConversations(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if p.UserID == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	mine, err := s.conversationsOf(r.Context(), p.UserID)
	if err != nil {
		s.internalError(w, "reading your conversations", err)
		return
	}
	for _, c := range mine {
		_ = s.docs.Delete(r.Context(), kindChat, c.ID)
	}
	s.auditReq(r, "chat.delete_all", "chat", "", true)
	w.WriteHeader(http.StatusNoContent)
}
