package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/forgelinehq/forgeline/internal/authn"
	"github.com/forgelinehq/forgeline/internal/oidcfake"
)

type ssoHarness struct {
	h     *harness
	admin *browser
	idp   *oidcfake.Provider
}

const ssoSecret = "s3cret-client-value"

func newSSOHarness(t *testing.T, extra map[string]any) *ssoHarness {
	t.Helper()
	h := newConsoleHarness(t)
	admin := h.adminBrowser("root")
	idp, err := oidcfake.New("forgeline-test", ssoSecret)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(idp.Handler())
	t.Cleanup(ts.Close)
	idp.Issuer = ts.URL
	body := map[string]any{"enabled": true, "name": "Acme", "issuer": ts.URL, "client_id": "forgeline-test", "client_secret": ssoSecret,
		"address": h.http.URL, "allow_new": true,
		"mappings": []map[string]string{{"group": "platform-admins", "role": "admin"}, {"group": "eng", "role": "developer"}}}
	for k, v := range extra {
		body[k] = v
	}
	admin.expect(200, "PUT", "/v1/settings/sso", body)
	return &ssoHarness{h: h, admin: admin, idp: idp}
}

func dana(groups ...string) *oidcfake.Identity {
	return &oidcfake.Identity{Subject: "u-1001", Email: "dana@acme.test", EmailVerified: true, Name: "Dana Example", Groups: groups}
}

// signIn follows the whole redirect chain in a browser and returns where it ended: "" for the console, or the
// sso-error code.
func (s *ssoHarness) signIn(b *browser) string {
	s.h.t.Helper()
	resp, _ := b.do("GET", "/v1/auth/oidc/start", nil)
	frag := resp.Request.URL.Fragment
	if strings.HasPrefix(frag, "sso-error=") {
		return strings.TrimPrefix(frag, "sso-error=")
	}
	if frag != "/" {
		s.h.t.Fatalf("ended at %s", resp.Request.URL)
	}
	return ""
}

func authOf(t *testing.T, b *browser) authStateJSON {
	t.Helper()
	var st authStateJSON
	json.Unmarshal(b.expect(200, "GET", "/v1/auth/state", nil), &st)
	return st
}

func TestSSOSignInMakesAnAccountAndFollowsGroups(t *testing.T) {
	s := newSSOHarness(t, nil)
	if st := authOf(t, s.h.browser()); !st.SSO.Enabled || st.SSO.Name != "Acme" {
		t.Fatalf("the sign-in page does not offer it: %+v", st.SSO)
	}
	s.idp.Set(dana("eng"), nil, nil)
	b := s.h.browser()
	if code := s.signIn(b); code != "" {
		t.Fatalf("refused: %s", code)
	}
	st := authOf(t, b)
	if !st.Authenticated || st.Role != "developer" || st.User == nil || !st.User.SSO || st.User.Username != "dana" || st.User.Email != "dana@acme.test" || st.User.Name != "Dana Example" {
		t.Fatalf("after sign-in: %+v %+v", st, st.User)
	}
	id := st.User.ID
	// The same person again, now in the admins group: the same account, with the new role.
	s.idp.Set(dana("platform-admins", "eng"), nil, nil)
	b2 := s.h.browser()
	if code := s.signIn(b2); code != "" {
		t.Fatalf("second sign-in refused: %s", code)
	}
	if st := authOf(t, b2); st.User.ID != id || st.Role != "admin" {
		t.Errorf("second sign-in: %+v", st)
	}
	// Out of every group, with no default role: refused.
	s.idp.Set(dana(), nil, nil)
	if code := s.signIn(s.h.browser()); code != "refused" {
		t.Errorf("no group, no default role: %q", code)
	}
	// An SSO account has no password; password sign-in fails like a wrong password.
	s.h.browser().expect(401, "POST", "/v1/auth/login", map[string]string{"username": "dana", "password": ""})
	s.h.browser().expect(401, "POST", "/v1/auth/login", map[string]string{"username": "dana", "password": "anything-at-all"})
	// Someone else whose username would clash gets a free one.
	s.idp.Set(&oidcfake.Identity{Subject: "u-2002", Email: "dana@other.test", EmailVerified: true, Groups: []string{"eng"}}, nil, nil)
	b3 := s.h.browser()
	if code := s.signIn(b3); code != "" {
		t.Fatalf("third person refused: %s", code)
	}
	if st := authOf(t, b3); st.User.Username != "dana-2" || st.User.ID == id {
		t.Errorf("clashing username: %+v", st.User)
	}
	// Someone with Dana's email but another subject (a provider that lets people choose their email) is a
	// different person: never Dana's account.
	s.idp.Set(&oidcfake.Identity{Subject: "u-6666", Email: "dana@acme.test", EmailVerified: true, Groups: []string{"platform-admins"}}, nil, nil)
	b4 := s.h.browser()
	if code := s.signIn(b4); code != "" {
		t.Fatalf("fourth person refused: %s", code)
	}
	if st := authOf(t, b4); st.User.ID == id {
		t.Errorf("a matching email took over Dana's account: %+v", st.User)
	}
	a := string(s.admin.expect(200, "GET", "/v1/audit", nil))
	for _, want := range []string{"login.sso", "user.create", "from groups at sign-in", "login.sso.failed"} {
		if !strings.Contains(a, want) {
			t.Errorf("audit has no %q", want)
		}
	}
}

// noFollow is a browser that stops at every redirect, so a test can carry the callback itself.
func noFollow(b *browser) *http.Client {
	return &http.Client{Jar: b.c.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// callbackURL starts a sign-in in b and walks to the provider, returning the callback address it sends back.
func (s *ssoHarness) callbackURL(b *browser) string {
	s.h.t.Helper()
	c := noFollow(b)
	resp, err := c.Get(s.h.http.URL + "/v1/auth/oidc/start")
	if err != nil || resp.StatusCode != http.StatusFound {
		s.h.t.Fatalf("start: %v %v", resp, err)
	}
	resp, err = c.Get(resp.Header.Get("Location"))
	if err != nil || resp.StatusCode != http.StatusFound {
		s.h.t.Fatalf("authorize: %v %v", resp, err)
	}
	return resp.Header.Get("Location")
}

func (s *ssoHarness) finish(b *browser, callback string) string {
	s.h.t.Helper()
	resp, err := noFollow(b).Get(callback)
	if err != nil {
		s.h.t.Fatal(err)
	}
	loc := resp.Header.Get("Location")
	if i := strings.Index(loc, "#sso-error="); i >= 0 {
		return loc[i+len("#sso-error="):]
	}
	if loc != "/#/" {
		s.h.t.Fatalf("callback went to %q", loc)
	}
	return ""
}

func TestSSORefusesBadOrReplayedSignIns(t *testing.T) {
	s := newSSOHarness(t, nil)
	other := oidcfake.NewKey()
	for name, tc := range map[string]struct {
		mutate func(map[string]any)
		key    bool
		want   string
	}{
		"wrong nonce":    {mutate: func(c map[string]any) { c["nonce"] = "not-the-one-sent" }, want: "token"},
		"wrong audience": {mutate: func(c map[string]any) { c["aud"] = "someone-else" }, want: "token"},
		"expired":        {mutate: func(c map[string]any) { c["exp"] = time.Now().Add(-10 * time.Minute).Unix() }, want: "token"},
		"wrong issuer":   {mutate: func(c map[string]any) { c["iss"] = "https://evil.example" }, want: "token"},
		"another key":    {key: true, want: "token"},
	} {
		var k = other
		if !tc.key {
			k = nil
		}
		s.idp.Set(dana("eng"), tc.mutate, k)
		b := s.h.browser()
		if code := s.signIn(b); code != tc.want {
			t.Errorf("%s: got %q, want %q", name, code, tc.want)
		}
		if authOf(t, b).Authenticated {
			t.Errorf("%s: signed in anyway", name)
		}
	}
	s.idp.Set(dana("eng"), nil, nil)

	// A callback carried to another browser (no state cookie there) is refused.
	a := s.h.browser()
	cb := s.callbackURL(a)
	jar, _ := cookiejar.New(nil)
	stranger := &browser{t: t, h: s.h, c: &http.Client{Jar: jar}, csr: true}
	if code := s.finish(stranger, cb); code != "state" {
		t.Errorf("another browser: %q", code)
	}
	// And it cannot be used afterwards by the right browser either: the state is gone.
	if code := s.finish(a, cb); code != "state" {
		t.Errorf("state used twice: %q", code)
	}
	// A good callback works once, and replaying it is refused.
	b := s.h.browser()
	cb = s.callbackURL(b)
	if code := s.finish(b, cb); code != "" {
		t.Fatalf("good callback refused: %q", code)
	}
	if code := s.finish(b, cb); code != "state" {
		t.Errorf("replayed callback: %q", code)
	}
	// A sign-in older than ten minutes is refused, even with the right cookie.
	b = s.h.browser()
	cb = s.callbackURL(b)
	u, _ := url.Parse(cb)
	key := hashHex(u.Query().Get("state"))
	var doc ssoStateDoc
	if err := s.h.srv.docs.Get(context.Background(), kindSSOState, key, &doc); err != nil {
		t.Fatal(err)
	}
	doc.CreatedAt = doc.CreatedAt.Add(-11 * time.Minute)
	s.h.srv.docs.Put(context.Background(), kindSSOState, key, doc)
	if code := s.finish(b, cb); code != "state" {
		t.Errorf("an old sign-in: %q", code)
	}
	// Made-up state.
	if code := s.finish(s.h.browser(), s.h.http.URL+"/v1/auth/oidc/callback?code=x&state=made-up"); code != "state" {
		t.Errorf("made-up state: %q", code)
	}
	// The provider saying no.
	if code := s.finish(s.h.browser(), s.h.http.URL+"/v1/auth/oidc/callback?error=access_denied"); code != "provider" {
		t.Errorf("provider error: %q", code)
	}
}

func TestSSORulesForNewPeopleDomainsAndRoles(t *testing.T) {
	// New people not let in.
	s := newSSOHarness(t, map[string]any{"allow_new": false})
	s.idp.Set(dana("eng"), nil, nil)
	if code := s.signIn(s.h.browser()); code != "refused" {
		t.Errorf("allow_new off: %q", code)
	}
	// Allowed domains: needs a verified email in one of them.
	s = newSSOHarness(t, map[string]any{"domains": []string{"@ACME.test"}})
	if v := s.admin.expect(200, "GET", "/v1/settings/sso", nil); !strings.Contains(string(v), `"domains":["acme.test"]`) {
		t.Errorf("domains not normalised: %s", v)
	}
	unverified := dana("eng")
	unverified.EmailVerified = false
	s.idp.Set(unverified, nil, nil)
	if code := s.signIn(s.h.browser()); code != "refused" {
		t.Errorf("unverified email: %q", code)
	}
	s.idp.Set(&oidcfake.Identity{Subject: "u-9", Email: "x@elsewhere.test", EmailVerified: true, Groups: []string{"eng"}}, nil, nil)
	if code := s.signIn(s.h.browser()); code != "refused" {
		t.Errorf("other domain: %q", code)
	}
	s.idp.Set(dana("eng"), nil, nil)
	if code := s.signIn(s.h.browser()); code != "" {
		t.Errorf("allowed domain refused: %q", code)
	}
	// A default role for people in no mapped group.
	s = newSSOHarness(t, map[string]any{"default_role": "viewer"})
	s.idp.Set(dana("marketing"), nil, nil)
	b := s.h.browser()
	if code := s.signIn(b); code != "" {
		t.Fatalf("default role refused: %q", code)
	}
	if st := authOf(t, b); st.Role != "viewer" {
		t.Errorf("default role: %+v", st)
	}
	// A disabled account cannot sign in by SSO.
	var users struct{ Users []UserJSON }
	json.Unmarshal(s.admin.expect(200, "GET", "/v1/users", nil), &users)
	var id string
	for _, u := range users.Users {
		if u.SSO {
			id = u.ID
		}
	}
	s.admin.expect(200, "PATCH", "/v1/users/"+id, map[string]any{"disabled": true})
	if code := s.signIn(s.h.browser()); code != "refused" {
		t.Errorf("disabled account: %q", code)
	}
}

func TestSSONeverDemotesTheLastAdministrator(t *testing.T) {
	s := newSSOHarness(t, nil)
	s.idp.Set(dana("platform-admins"), nil, nil)
	b := s.h.browser()
	if code := s.signIn(b); code != "" {
		t.Fatal(code)
	}
	// Dana demotes the setup account, so Dana is the only administrator left.
	b.expect(200, "PATCH", "/v1/users/root", map[string]any{"role": "viewer"})
	s.idp.Set(dana("eng"), nil, nil)
	b2 := s.h.browser()
	if code := s.signIn(b2); code != "" {
		t.Fatal(code)
	}
	if st := authOf(t, b2); st.Role != "admin" {
		t.Errorf("the last administrator was demoted by a group change: %+v", st)
	}
	if !strings.Contains(string(b2.expect(200, "GET", "/v1/audit", nil)), "this is the last administrator") {
		t.Error("keeping the last administrator was not audited")
	}
}

func TestSSOSettingsAreForAdministratorsAndKeepTheSecret(t *testing.T) {
	s := newSSOHarness(t, nil)
	s.admin.createUser("dev", authn.Developer)
	dev := s.h.login("dev")
	dev.expect(403, "GET", "/v1/settings/sso", nil)
	dev.expect(403, "PUT", "/v1/settings/sso", map[string]any{"enabled": false})
	v := string(s.admin.expect(200, "GET", "/v1/settings/sso", nil))
	if strings.Contains(v, ssoSecret) || !strings.Contains(v, `"has_secret":true`) || !strings.Contains(v, `"redirect_url":"`+s.h.http.URL+`/v1/auth/oidc/callback"`) {
		t.Errorf("settings view: %s", v)
	}
	if strings.Contains(string(s.admin.expect(200, "GET", "/v1/audit", nil)), ssoSecret) {
		t.Error("the secret is in the audit log")
	}
	cur := map[string]any{"enabled": true, "issuer": s.idp.Issuer, "client_id": "forgeline-test", "address": s.h.http.URL, "allow_new": true, "default_role": "viewer"}
	with := func(k string, val any) map[string]any {
		m := map[string]any{}
		for a, b := range cur {
			m[a] = b
		}
		m[k] = val
		return m
	}
	s.admin.expect(400, "PUT", "/v1/settings/sso", with("issuer", "http://idp.example.com"))
	s.admin.expect(400, "PUT", "/v1/settings/sso", with("address", "http://forgeline.example.com"))
	s.admin.expect(400, "PUT", "/v1/settings/sso", with("issuer", ""))
	s.admin.expect(400, "PUT", "/v1/settings/sso", with("mappings", []map[string]string{{"group": "x", "role": "root"}}))
	s.admin.expect(400, "PUT", "/v1/settings/sso", with("domains", []string{"not a domain"}))
	s.admin.expect(400, "PUT", "/v1/settings/sso", with("default_role", "owner"))
	// Saving without a secret keeps the stored one.
	s.admin.expect(200, "PUT", "/v1/settings/sso", cur)
	s.idp.Set(dana("eng"), nil, nil)
	if code := s.signIn(s.h.browser()); code != "" {
		t.Errorf("the secret was lost on save: %q", code)
	}
	// Turned off: no button, and start goes back to the sign-in page.
	s.admin.expect(200, "PUT", "/v1/settings/sso", with("enabled", false))
	if st := authOf(t, s.h.browser()); st.SSO.Enabled {
		t.Error("the button is still offered")
	}
	if code := s.signIn(s.h.browser()); code != "off" {
		t.Errorf("start while off: %q", code)
	}
	// Test fetches the discovery document.
	var res map[string]any
	json.Unmarshal(s.admin.expect(200, "POST", "/v1/settings/sso/test", map[string]string{"issuer": s.idp.Issuer}), &res)
	if res["ok"] != true || res["pkce"] != true {
		t.Errorf("test: %v", res)
	}
	s.admin.expect(400, "POST", "/v1/settings/sso/test", map[string]string{"issuer": "http://idp.example.com"})
	// Enabling with no secret ever stored is refused.
	h2 := newConsoleHarness(t)
	a2 := h2.adminBrowser("root")
	a2.expect(400, "PUT", "/v1/settings/sso", map[string]any{"enabled": true, "issuer": s.idp.Issuer, "client_id": "x", "address": h2.http.URL})
}
