package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/docstore"
)

// sweepSSOStates drops sign-ins that were started and never finished.
func (s *Server) sweepSSOStates(ctx context.Context) {
	docs, err := s.docs.List(ctx, kindSSOState, docstore.ListOptions{})
	if err != nil {
		return
	}
	for _, d := range docs {
		var st ssoStateDoc
		if json.Unmarshal(d.Body, &st) == nil && time.Since(st.CreatedAt) > ssoStateTTL {
			_ = s.docs.Delete(ctx, kindSSOState, d.ID)
		}
	}
}

// Single sign-on with OpenID Connect (design note 031). A person signs in at the company's provider and comes
// back with an ID token; Forgeline checks it (signature against the provider's keys, issuer, audience, expiry and
// the nonce it sent), finds or makes the account for that (issuer, subject), applies the role from the person's
// groups, and starts the same session a password sign-in would. Passwords stay for local accounts.

const (
	idSSO          = "sso"
	ssoSecretName  = "sso.client_secret" // internal secret: sealed in the vault, never shown
	kindSSOState   = "ssostate"
	ssoStateCookie = "forgeline_sso"
	ssoStateTTL    = 10 * time.Minute
	ssoCallback    = "/v1/auth/oidc/callback"
)

type ssoMapping struct {
	Group string     `json:"group"`
	Role  authn.Role `json:"role"`
}

type ssoSettings struct {
	Enabled     bool         `json:"enabled"`
	Name        string       `json:"name"`         // the button: "Sign in with <name>"
	Issuer      string       `json:"issuer"`       // the provider's issuer URL
	ClientID    string       `json:"client_id"`    // registered at the provider
	Address     string       `json:"address"`      // where people reach Forgeline; the callback is under it
	GroupsClaim string       `json:"groups_claim"` // default "groups"
	Mappings    []ssoMapping `json:"mappings"`
	AllowNew    bool         `json:"allow_new"`    // make an account at a person's first sign-in
	DefaultRole authn.Role   `json:"default_role"` // when no group matches; empty means refuse
	Domains     []string     `json:"domains"`      // allowed email domains; empty means any
	UpdatedAt   time.Time    `json:"updated_at"`
	UpdatedBy   string       `json:"updated_by"`
}

type ssoStateDoc struct {
	Nonce     string    `json:"nonce"`
	Verifier  string    `json:"verifier"`
	Binding   string    `json:"binding"` // sha256 of the browser's state cookie
	CreatedAt time.Time `json:"created_at"`
}

func (s *Server) loadOIDC(ctx context.Context) ssoSettings {
	var st ssoSettings
	_ = s.docs.Get(ctx, kindSettings, idSSO, &st)
	if st.GroupsClaim == "" {
		st.GroupsClaim = "groups"
	}
	return st
}

// ssoURLOK: https, or http only to a loopback host (trying it on a laptop).
func ssoURLOK(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	return u.Scheme == "https" || u.Scheme == "http" && isLoopbackHost(u.Hostname())
}

func (st ssoSettings) redirectURL() string { return strings.TrimRight(st.Address, "/") + ssoCallback }

// ready says whether sign-in can start: switched on and complete, with the secret stored.
func (s *Server) ssoReady(ctx context.Context) (ssoSettings, string, bool) {
	st := s.loadOIDC(ctx)
	if !st.Enabled || st.Issuer == "" || st.ClientID == "" || st.Address == "" {
		return st, "", false
	}
	secret, ok := s.internalSecretValue(ctx, ssoSecretName)
	return st, secret, ok && secret != ""
}

func ssoContext(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, &http.Client{Timeout: 15 * time.Second})
}

func (s *Server) ssoProvider(ctx context.Context, st ssoSettings) (*oidc.Provider, error) {
	return oidc.NewProvider(ssoContext(ctx), st.Issuer)
}

func (st ssoSettings) oauth(p *oidc.Provider, secret string) *oauth2.Config {
	return &oauth2.Config{ClientID: st.ClientID, ClientSecret: secret, Endpoint: p.Endpoint(), RedirectURL: st.redirectURL(),
		Scopes: []string{oidc.ScopeOpenID, "email", "profile"}}
}

func randomToken() string {
	t, _, err := authn.NewToken()
	if err != nil {
		panic(err) // crypto/rand failing is not something to carry on from
	}
	return t
}

func hashHex(v string) string { s := sha256.Sum256([]byte(v)); return hex.EncodeToString(s[:]) }

// handleOIDCStart sends the browser to the provider. GET, no session: the sign-in page links here.
func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	st, secret, ok := s.ssoReady(r.Context())
	if !ok {
		http.Redirect(w, r, "/#sso-error=off", http.StatusFound)
		return
	}
	p, err := s.ssoProvider(r.Context(), st)
	if err != nil {
		s.audit(r.Context(), Principal{}, clientIP(r), "login.sso.failed", "", "the provider could not be reached: "+err.Error(), false)
		http.Redirect(w, r, "/#sso-error=provider", http.StatusFound)
		return
	}
	s.sweepSSOStates(r.Context())
	state, nonce, binding, verifier := randomToken(), randomToken(), randomToken(), oauth2.GenerateVerifier()
	doc := ssoStateDoc{Nonce: nonce, Verifier: verifier, Binding: hashHex(binding), CreatedAt: time.Now().UTC()}
	if err := s.docs.Put(r.Context(), kindSSOState, hashHex(state), doc); err != nil {
		s.internalError(w, "starting sign-in", err)
		return
	}
	// Lax, not Strict: the provider sends the person back with a cross-site redirect, and this cookie must come along.
	http.SetCookie(w, &http.Cookie{Name: ssoStateCookie, Value: binding, Path: "/v1/auth/oidc/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: s.cookieSecure(r), MaxAge: int(ssoStateTTL.Seconds())})
	http.Redirect(w, r, st.oauth(p, secret).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

// ssoClaims is what Forgeline reads from the ID token.
type ssoClaims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Username      string
	Groups        []string
}

// handleOIDCCallback finishes sign-in. Every refusal ends on the sign-in page with a short code, and is audited
// with the full reason.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ip := clientIP(r)
	fail := func(code, reason string) {
		s.audit(ctx, Principal{}, ip, "login.sso.failed", "", reason, false)
		http.SetCookie(w, &http.Cookie{Name: ssoStateCookie, Value: "", Path: "/v1/auth/oidc/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		http.Redirect(w, r, "/#sso-error="+code, http.StatusFound)
	}
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		fail("provider", "the provider said "+truncate(e, 80)+": "+truncate(q.Get("error_description"), 200))
		return
	}
	state := q.Get("state")
	if state == "" || q.Get("code") == "" {
		fail("state", "the provider sent no code or state")
		return
	}
	// The state is good once: read it and delete it before anything else can use it.
	var doc ssoStateDoc
	key := hashHex(state)
	if err := s.docs.Get(ctx, kindSSOState, key, &doc); err != nil {
		fail("state", "unknown or already used state")
		return
	}
	_ = s.docs.Delete(ctx, kindSSOState, key)
	c, err := r.Cookie(ssoStateCookie)
	if err != nil || subtle.ConstantTimeCompare([]byte(hashHex(c.Value)), []byte(doc.Binding)) != 1 {
		fail("state", "the sign-in was started in another browser (state cookie missing or different)")
		return
	}
	if time.Since(doc.CreatedAt) > ssoStateTTL {
		fail("state", "the sign-in took longer than 10 minutes")
		return
	}
	st, secret, ok := s.ssoReady(ctx)
	if !ok {
		fail("off", "single sign-on is not set up")
		return
	}
	p, err := s.ssoProvider(ctx, st)
	if err != nil {
		fail("provider", "the provider could not be reached: "+err.Error())
		return
	}
	tok, err := st.oauth(p, secret).Exchange(ssoContext(ctx), q.Get("code"), oauth2.VerifierOption(doc.Verifier))
	if err != nil {
		fail("token", "the code exchange failed: "+truncate(err.Error(), 200))
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		fail("token", "the provider returned no ID token")
		return
	}
	idt, err := p.Verifier(&oidc.Config{ClientID: st.ClientID, SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256}}).Verify(ssoContext(ctx), raw)
	if err != nil {
		fail("token", "the ID token was refused: "+truncate(err.Error(), 200))
		return
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(doc.Nonce)) != 1 {
		fail("token", "the ID token's nonce does not match")
		return
	}
	var all map[string]any
	if err := idt.Claims(&all); err != nil {
		fail("token", "the ID token's claims could not be read")
		return
	}
	cl := readClaims(idt.Subject, all, st.GroupsClaim)
	u, why := s.ssoAccount(ctx, st, idt.Issuer, cl, ip)
	if why != "" {
		fail("refused", why)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: ssoStateCookie, Value: "", Path: "/v1/auth/oidc/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	if err := s.startSession(w, r, u); err != nil {
		s.internalError(w, "starting a session", err)
		return
	}
	s.audit(ctx, Principal{Name: u.Username, Role: u.Role}, ip, "login.sso", "user/"+u.ID, "signed in with "+st.Name, true)
	http.Redirect(w, r, "/#/", http.StatusFound)
}

func readClaims(sub string, all map[string]any, groupsClaim string) ssoClaims {
	str := func(k string) string { v, _ := all[k].(string); return strings.TrimSpace(v) }
	c := ssoClaims{Subject: sub, Email: strings.ToLower(str("email")), Name: str("name"), Username: str("preferred_username")}
	switch v := all["email_verified"].(type) {
	case bool:
		c.EmailVerified = v
	case string: // a few providers send "true"
		c.EmailVerified = v == "true"
	}
	switch g := all[groupsClaim].(type) {
	case []any:
		for _, x := range g {
			if s, ok := x.(string); ok && s != "" {
				c.Groups = append(c.Groups, s)
			}
		}
	case string:
		if g != "" {
			c.Groups = []string{g}
		}
	}
	return c
}

// roleFor is the highest role a person's groups map to, else the default role, else "" (no role: refused).
func (st ssoSettings) roleFor(groups []string) authn.Role {
	in := map[string]bool{}
	for _, g := range groups {
		in[g] = true
	}
	best := authn.Role("")
	for _, m := range st.Mappings {
		if in[m.Group] && rank(m.Role) > rank(best) {
			best = m.Role
		}
	}
	if best == "" {
		best = st.DefaultRole
	}
	return best
}

func rank(r authn.Role) int {
	return map[authn.Role]int{authn.Viewer: 1, authn.Developer: 2, authn.Admin: 3}[r]
}

var usernameClean = regexp.MustCompile(`[^a-z0-9._-]+`)

// ssoAccount finds or makes the account for (issuer, subject) and applies the rules. why is the reason for a
// refusal, for the audit log; the person only sees that they were refused.
func (s *Server) ssoAccount(ctx context.Context, st ssoSettings, issuer string, cl ssoClaims, ip string) (User, string) {
	if cl.Subject == "" {
		return User{}, "the ID token has no subject"
	}
	if len(st.Domains) > 0 {
		at := strings.LastIndex(cl.Email, "@")
		if at < 0 || !cl.EmailVerified || !contains(st.Domains, cl.Email[at+1:]) {
			return User{}, fmt.Sprintf("email %q is not verified or not in an allowed domain", cl.Email)
		}
	}
	role := st.roleFor(cl.Groups)
	users, err := s.loadUsers(ctx)
	if err != nil {
		return User{}, "the accounts could not be read"
	}
	var u User
	found := false
	for _, x := range users {
		if x.SSOIssuer == issuer && x.SSOSubject == cl.Subject {
			u, found = x, true
			break
		}
	}
	now := time.Now().UTC()
	actor := Principal{Name: "sso"}
	if !found {
		switch {
		case !st.AllowNew:
			return User{}, "new people are not let in by single sign-on; an administrator can turn that on"
		case role == "":
			return User{}, fmt.Sprintf("no role applies to groups [%s] and there is no default role", strings.Join(cl.Groups, ", "))
		}
		u = User{ID: s.freeUsername(users, cl), Name: cl.Name, Role: role, CreatedAt: now, CreatedBy: "sso", SSOIssuer: issuer, SSOSubject: cl.Subject, Email: cl.Email}
		u.Username = u.ID
		u.LastLoginAt = &now
		if err := s.docs.Create(ctx, kindUser, u.ID, u); err != nil {
			return User{}, "the account could not be created: " + err.Error()
		}
		_ = s.audit(ctx, actor, ip, "user.create", "user/"+u.ID, "made at first single sign-on, role "+string(role), true)
		return u, ""
	}
	if u.Disabled {
		return User{}, "the account " + u.Username + " is disabled"
	}
	if role == "" {
		return User{}, fmt.Sprintf("no role applies to groups [%s] any more and there is no default role", strings.Join(cl.Groups, ", "))
	}
	if role != u.Role {
		// Never demote the last active administrator, or the server could end up with nobody to run it.
		if u.Role == authn.Admin {
			if n, err := s.activeAdmins(ctx); err != nil || n <= 1 {
				_ = s.audit(ctx, actor, ip, "user.role", "user/"+u.ID, "kept as admin: the groups say "+string(role)+", but this is the last administrator", false)
				role = authn.Admin
			}
		}
		if role != u.Role {
			_ = s.audit(ctx, actor, ip, "user.role", "user/"+u.ID, "role "+string(u.Role)+" → "+string(role)+" from groups at sign-in", true)
			u.Role = role
		}
	}
	if cl.Name != "" {
		u.Name = cl.Name
	}
	if cl.Email != "" {
		u.Email = cl.Email
	}
	u.LastLoginAt = &now
	if err := s.docs.Put(ctx, kindUser, u.ID, u); err != nil {
		return User{}, "the account could not be updated"
	}
	return u, ""
}

// freeUsername picks a username from the token that no account has yet.
func (s *Server) freeUsername(users []User, cl ssoClaims) string {
	base := cl.Username
	if base == "" && cl.Email != "" {
		base = cl.Email[:strings.Index(cl.Email+"@", "@")]
	}
	base = strings.Trim(usernameClean.ReplaceAllString(strings.ToLower(base), "-"), "-._")
	if len(base) > 32 {
		base = base[:32]
	}
	if len(base) < 2 {
		base = "user"
	}
	taken := map[string]bool{}
	for _, u := range users {
		taken[u.ID] = true
	}
	name := base
	for i := 2; taken[name]; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}

// --- settings ---

type SSOSettingsJSON struct {
	ssoSettings
	HasSecret   bool   `json:"has_secret"`
	RedirectURL string `json:"redirect_url"`
	Ready       bool   `json:"ready"`
}

func (s *Server) ssoView(ctx context.Context) SSOSettingsJSON {
	st := s.loadOIDC(ctx)
	_, has := s.internalSecretValue(ctx, ssoSecretName)
	_, _, ready := s.ssoReady(ctx)
	v := SSOSettingsJSON{ssoSettings: st, HasSecret: has, Ready: ready}
	if st.Address != "" {
		v.RedirectURL = st.redirectURL()
	}
	if v.Mappings == nil {
		v.Mappings = []ssoMapping{}
	}
	if v.Domains == nil {
		v.Domains = []string{}
	}
	return v
}

func (s *Server) handleGetSSO(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.ssoView(r.Context()))
}

var domainRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

func (s *Server) handlePutSSO(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ssoSettings
		ClientSecret string `json:"client_secret"` // write-only; empty keeps the stored one
	}
	if !readJSON(w, r, &req) {
		return
	}
	st := req.ssoSettings
	st.Name, st.Issuer, st.ClientID = strings.TrimSpace(st.Name), strings.TrimRight(strings.TrimSpace(st.Issuer), "/"), strings.TrimSpace(st.ClientID)
	st.Address, st.GroupsClaim = strings.TrimRight(strings.TrimSpace(st.Address), "/"), strings.TrimSpace(st.GroupsClaim)
	if st.Name == "" {
		st.Name = "your company login"
	}
	if st.GroupsClaim == "" {
		st.GroupsClaim = "groups"
	}
	bad := func(msg string) { writeError(w, http.StatusBadRequest, msg) }
	switch {
	case st.Issuer != "" && !ssoURLOK(st.Issuer):
		bad("issuer: use an https address (http only for a provider on this machine)")
		return
	case st.Address != "" && !ssoURLOK(st.Address):
		bad("address: use the https address people reach Forgeline at (http only on this machine)")
		return
	case st.Enabled && (st.Issuer == "" || st.ClientID == "" || st.Address == ""):
		bad("to turn single sign-on on, fill in the issuer, the client ID and the address")
		return
	case st.DefaultRole != "" && rank(st.DefaultRole) == 0:
		bad("default_role must be viewer, developer, admin or empty")
		return
	case len(st.Name) > 40 || len(st.ClientID) > 300 || len(st.GroupsClaim) > 60 || len(st.Mappings) > 50 || len(st.Domains) > 20:
		bad("a field is too long")
		return
	}
	for i, m := range st.Mappings {
		m.Group = strings.TrimSpace(m.Group)
		if m.Group == "" || len(m.Group) > 200 || rank(m.Role) == 0 {
			bad(fmt.Sprintf("mappings[%d]: give a group and a role (viewer, developer or admin)", i))
			return
		}
		st.Mappings[i] = m
	}
	for i, d := range st.Domains {
		d = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(d, "@")))
		if !domainRE.MatchString(d) {
			bad(fmt.Sprintf("domains[%d]: %q is not a domain", i, d))
			return
		}
		st.Domains[i] = d
	}
	sort.Strings(st.Domains)
	ctx := r.Context()
	who := principalFrom(ctx).Name
	if req.ClientSecret != "" {
		if _, _, err := s.putSecret(ctx, who, ssoSecretName, req.ClientSecret, "single sign-on client secret"); err != nil {
			s.internalError(w, "storing the client secret", err)
			return
		}
	} else if _, has := s.internalSecretValue(ctx, ssoSecretName); st.Enabled && !has {
		bad("to turn single sign-on on, give the client secret")
		return
	}
	st.UpdatedAt, st.UpdatedBy = time.Now().UTC(), who
	if err := s.docs.Put(ctx, kindSettings, idSSO, st); err != nil {
		s.internalError(w, "saving the settings", err)
		return
	}
	detail := fmt.Sprintf("enabled %v, issuer %s, %d group mappings, new people %v, default role %q, domains [%s], secret changed %v",
		st.Enabled, st.Issuer, len(st.Mappings), st.AllowNew, st.DefaultRole, strings.Join(st.Domains, ", "), req.ClientSecret != "")
	s.auditReq(r, "sso.update", "settings/sso", detail, true)
	writeJSON(w, http.StatusOK, s.ssoView(ctx))
}

// handleTestSSO fetches the provider's discovery document and says what it found. It changes nothing.
func (s *Server) handleTestSSO(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Issuer string `json:"issuer"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	issuer := strings.TrimRight(strings.TrimSpace(req.Issuer), "/")
	if !ssoURLOK(issuer) {
		writeError(w, http.StatusBadRequest, "issuer: use an https address (http only for a provider on this machine)")
		return
	}
	p, err := oidc.NewProvider(ssoContext(r.Context()), issuer)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": truncate(err.Error(), 300)})
		return
	}
	var meta struct {
		Auth   string   `json:"authorization_endpoint"`
		Token  string   `json:"token_endpoint"`
		PKCE   []string `json:"code_challenge_methods_supported"`
		Scopes []string `json:"scopes_supported"`
	}
	_ = p.Claims(&meta)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "authorization_endpoint": meta.Auth, "token_endpoint": meta.Token,
		"pkce": contains(meta.PKCE, "S256"), "scopes": meta.Scopes})
}

// ssoPublic is what the sign-in page needs: whether to show the button, and its words.
type ssoPublic struct {
	Enabled bool   `json:"enabled"`
	Name    string `json:"name,omitempty"`
}

func (s *Server) ssoPublicState(ctx context.Context) ssoPublic {
	st, _, ok := s.ssoReady(ctx)
	if !ok {
		return ssoPublic{}
	}
	return ssoPublic{Enabled: true, Name: st.Name}
}
