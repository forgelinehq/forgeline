// Package oidcfake is a small OpenID Connect provider for tests and for trying single sign-on on a laptop
// (scripts/dev/fakeoidc). It is not a real identity provider: it signs whoever it is told to, and it keeps
// everything in memory. It implements discovery, the published keys, the authorization endpoint (approving at
// once in tests, or with a form for people) and the token endpoint with PKCE (S256) and a client secret.
package oidcfake

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// Identity is who the provider says signed in.
type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Username      string
	Groups        []string
}

// Provider is the stand-in. Set the exported fields before use; they are read under a lock.
type Provider struct {
	Issuer       string // its own base URL, set by the caller once it knows it
	ClientID     string
	ClientSecret string

	mu sync.Mutex
	// Next is who the authorization endpoint signs in without asking. When it is nil the endpoint shows a form.
	Next *Identity
	// Mutate, when set, changes the ID token's claims before signing (to make a bad token in a test).
	Mutate func(claims map[string]any)
	// SignWith, when set, signs with another key than the published one.
	SignWith *rsa.PrivateKey

	key   *rsa.PrivateKey
	codes map[string]grant
}

type grant struct {
	who       Identity
	nonce     string
	challenge string
	redirect  string
	at        time.Time
}

// New makes a provider with a fresh 2048-bit key.
func New(clientID, clientSecret string) (*Provider, error) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &Provider{ClientID: clientID, ClientSecret: clientSecret, key: k, codes: map[string]grant{}}, nil
}

// NewKey returns another key, for SignWith.
func NewKey() *rsa.PrivateKey { k, _ := rsa.GenerateKey(rand.Reader, 2048); return k }

// Set changes the test knobs together.
func (p *Provider) Set(next *Identity, mutate func(map[string]any), signWith *rsa.PrivateKey) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Next, p.Mutate, p.SignWith = next, mutate, signWith
}

// Handler serves the provider's endpoints.
func (p *Provider) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /keys", p.keys)
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("POST /authorize", p.approve)
	mux.HandleFunc("POST /token", p.token)
	return mux
}

func (p *Provider) discovery(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"issuer": p.Issuer, "authorization_endpoint": p.Issuer + "/authorize", "token_endpoint": p.Issuer + "/token",
		"jwks_uri": p.Issuer + "/keys", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"}, "scopes_supported": []string{"openid", "email", "profile"},
		"code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
	})
}

func (p *Provider) keys(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
}

var form = template.Must(template.New("f").Parse(`<!doctype html><meta charset="utf-8"><title>Stand-in sign-in</title>
<body style="font-family:sans-serif;max-width:28rem;margin:3rem auto"><h1>Stand-in company login</h1>
<p>This is scripts/dev/fakeoidc, not a real provider. Choose who signs in.</p>
<form method="post"><input type="hidden" name="q" value="{{.Q}}">
<p><label>Subject (stable id)<br><input name="sub" value="u-1001" required></label></p>
<p><label>Email<br><input name="email" value="dana@example.com"></label> <label><input type="checkbox" name="verified" checked> verified</label></p>
<p><label>Name<br><input name="name" value="Dana Example"></label></p>
<p><label>Groups, comma separated<br><input name="groups" value="platform-admins"></label></p>
<p><button>Sign in</button></p></form>`))

func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != p.ClientID || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		http.Error(w, "bad authorization request", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	next := p.Next
	p.mu.Unlock()
	if next == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		form.Execute(w, map[string]string{"Q": r.URL.RawQuery})
		return
	}
	p.redirect(w, r, q, *next)
}

func (p *Provider) approve(w http.ResponseWriter, r *http.Request) {
	q, err := url.ParseQuery(r.PostFormValue("q"))
	if err != nil || q.Get("client_id") != p.ClientID {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var groups []string
	for _, g := range strings.Split(r.PostFormValue("groups"), ",") {
		if g = strings.TrimSpace(g); g != "" {
			groups = append(groups, g)
		}
	}
	p.redirect(w, r, q, Identity{Subject: r.PostFormValue("sub"), Email: r.PostFormValue("email"), EmailVerified: r.PostFormValue("verified") != "",
		Name: r.PostFormValue("name"), Groups: groups})
}

func (p *Provider) redirect(w http.ResponseWriter, r *http.Request, q url.Values, who Identity) {
	code := random()
	p.mu.Lock()
	p.codes[code] = grant{who: who, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), at: time.Now()}
	p.mu.Unlock()
	u, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}
	v := u.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	u.RawQuery = v.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostFormValue("client_id"), r.PostFormValue("client_secret")
	}
	if id != p.ClientID || subtle.ConstantTimeCompare([]byte(secret), []byte(p.ClientSecret)) != 1 {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]string{"error": "invalid_client"})
		return
	}
	p.mu.Lock()
	g, found := p.codes[r.PostFormValue("code")]
	delete(p.codes, r.PostFormValue("code")) // a code is good once
	mutate, signWith := p.Mutate, p.SignWith
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostFormValue("code_verifier")))
	if !found || time.Since(g.at) > time.Minute || g.redirect != r.PostFormValue("redirect_uri") || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant"})
		return
	}
	now := time.Now()
	claims := map[string]any{"iss": p.Issuer, "aud": p.ClientID, "sub": g.who.Subject, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"nonce": g.nonce, "email": g.who.Email, "email_verified": g.who.EmailVerified, "name": g.who.Name}
	if g.who.Username != "" {
		claims["preferred_username"] = g.who.Username
	}
	if g.who.Groups != nil {
		claims["groups"] = g.who.Groups
	}
	if mutate != nil {
		mutate(claims)
	}
	key := p.key
	if signWith != nil {
		key = signWith
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	payload, _ := json.Marshal(claims)
	jws, err := signer.Sign(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tok, _ := jws.CompactSerialize()
	writeJSON(w, map[string]any{"access_token": random(), "token_type": "Bearer", "expires_in": 300, "id_token": tok})
}

func random() string {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	return base64.RawURLEncoding.EncodeToString(n.Bytes())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
