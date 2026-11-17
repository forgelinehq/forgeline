package server

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/forgelinehq/forgeline/internal/docstore"
	"github.com/forgelinehq/forgeline/internal/guardrail"
	"github.com/forgelinehq/forgeline/internal/plugin"
)

// The library (design note 040, step 8) grows out of the marketplace: besides skills, agents, workflows, plugins and
// environment templates, a package may hold guardrail packs and MCP servers (tools). Installing one creates the same
// things an administrator could make by hand, in a state that changes nothing until they act: a policy pack is
// installed turned off, and an MCP server with no token and none of its tools turned on.

// MarketMCPServer is an MCP server a package offers.
type MarketMCPServer struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"` // remote or local
	URL         string `json:"url,omitempty"`
	AuthHeader  string `json:"auth_header,omitempty"`
	AuthScheme  string `json:"auth_scheme,omitempty"`
	// Token says, in words, what token the server needs ("a GitHub token that can read repositories"); empty when
	// it needs none.
	Token   string   `json:"token,omitempty"`
	Image   string   `json:"image,omitempty"`
	Network bool     `json:"network,omitempty"`
	Hosts   []string `json:"hosts,omitempty"`
	Docs    string   `json:"docs,omitempty"` // where its tools are described, https
}

var marketKeyRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,29}$`)

func (m MarketMCPServer) check() error {
	at := fmt.Sprintf("mcp server %q", m.Key)
	if !marketKeyRE.MatchString(m.Key) {
		return fmt.Errorf("%s: its key is lower-case letters, digits and dashes, at most 30", at)
	}
	if msg := checkNameDesc(m.Name, m.Description); msg != "" {
		return fmt.Errorf("%s: %s", at, msg)
	}
	if len(m.Token) > 200 {
		return fmt.Errorf("%s: say what token it needs in at most 200 characters", at)
	}
	if m.Docs != "" {
		if u, err := url.Parse(m.Docs); err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("%s: docs is an https address", at)
		}
	}
	switch m.Kind {
	case mcpRemote:
		u, err := url.Parse(m.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
			return fmt.Errorf("%s: a remote server's url is a full https address", at)
		}
		if isLoopbackHost(u.Hostname()) {
			return fmt.Errorf("%s: a package cannot point at this machine", at)
		}
		if m.Image != "" || m.Network || len(m.Hosts) > 0 {
			return fmt.Errorf("%s: a remote server has no image or network of its own", at)
		}
		if m.Token != "" && !headerNameRE.MatchString(m.AuthHeader) {
			return fmt.Errorf("%s: name the header its token goes in, such as Authorization", at)
		}
		if strings.ContainsAny(m.AuthScheme, " \t\r\n:") || len(m.AuthScheme) > 20 {
			return fmt.Errorf("%s: auth_scheme is one word such as Bearer", at)
		}
	case mcpLocal:
		if m.URL != "" || m.AuthHeader != "" || m.AuthScheme != "" {
			return fmt.Errorf("%s: a local server runs as a container; it has no address or token header", at)
		}
		spec := plugin.Spec{ID: "mcp-" + m.Key, Image: m.Image, Network: m.Network, Hosts: m.Hosts, Env: map[string]string{}}
		if err := spec.Check(); err != nil {
			return fmt.Errorf("%s: %v", at, err)
		}
	default:
		return fmt.Errorf(`%s: kind is "remote" or "local"`, at)
	}
	return nil
}

// checkLibrary checks what the library adds to a package: who reviewed it, its guardrail packs and its MCP servers.
func (p MarketPackage) checkLibrary() error {
	if len(p.ReviewedBy) > 80 || strings.ContainsAny(p.ReviewedBy, "\r\n") {
		return errors.New("reviewed_by: at most 80 characters on one line")
	}
	if len(p.PolicyPacks) > guardrail.MaxPacks {
		return fmt.Errorf("a package holds at most %d policy packs", guardrail.MaxPacks)
	}
	ids := map[string]bool{}
	for _, pk := range p.PolicyPacks {
		if err := pk.Validate(); err != nil {
			return fmt.Errorf("policy pack %q: %v", pk.ID, err)
		}
		if pk.Enabled {
			return fmt.Errorf("policy pack %q: a packaged pack is installed turned off; leave enabled out", pk.ID)
		}
		if ids[pk.ID] {
			return fmt.Errorf("policy pack %q is repeated", pk.ID)
		}
		ids[pk.ID] = true
	}
	keys := map[string]bool{}
	for _, m := range p.MCPServers {
		if err := m.check(); err != nil {
			return err
		}
		if keys[m.Key] {
			return fmt.Errorf("mcp server %q is repeated", m.Key)
		}
		keys[m.Key] = true
	}
	return nil
}

// libraryConflicts says why the package's packs or MCP servers cannot be installed, before anything is written: a
// name someone else uses, or too many of either.
func (s *Server) libraryConflicts(ctx context.Context, in MarketInstall, pkg MarketPackage) (string, error) {
	if len(pkg.PolicyPacks) > 0 {
		set, err := s.loadPolicyPacks(ctx)
		if err != nil {
			return "", err
		}
		mine, offered := map[string]bool{}, map[string]bool{}
		for _, id := range in.PolicyPacks {
			mine[id] = true
		}
		for _, pk := range pkg.PolicyPacks {
			offered[pk.ID] = true
		}
		n := 0
		for _, cur := range set.Packs {
			if offered[cur.ID] && !mine[cur.ID] {
				return fmt.Sprintf("a policy pack %q already exists; rename or remove it first", cur.ID), nil
			}
			if !mine[cur.ID] {
				n++
			}
		}
		if n+len(pkg.PolicyPacks) > guardrail.MaxPacks {
			return fmt.Sprintf("there would be more than %d policy packs; remove one first", guardrail.MaxPacks), nil
		}
	}
	if len(pkg.MCPServers) > 0 {
		all, err := s.listMCPServers(ctx)
		if err != nil {
			return "", err
		}
		exists := map[string]bool{}
		for _, srv := range all {
			exists[srv.ID] = true
		}
		mine := map[string]bool{}
		for _, id := range in.MCPServers {
			mine[id] = true
		}
		added := 0
		for _, m := range pkg.MCPServers {
			id, ok := in.MCPServers[m.Key]
			if !ok {
				id = m.Key
			}
			if exists[id] && !mine[id] {
				return fmt.Sprintf("an MCP server %q already exists; rename or remove it first", id), nil
			}
			if !exists[id] {
				added++
			}
		}
		if len(all)+added > maxMCPServers {
			return fmt.Sprintf("at most %d MCP servers", maxMCPServers), nil
		}
	}
	return "", nil
}

// installPolicyPacks adds the package's packs to the settings, turned off, or updates the ones an earlier install
// added (keeping whether an administrator turned them on). A pack id someone else uses is refused.
func (s *Server) installPolicyPacks(ctx context.Context, in *MarketInstall, pkg MarketPackage, by string, now time.Time) error {
	if len(pkg.PolicyPacks) == 0 && len(in.PolicyPacks) == 0 {
		return nil
	}
	set, err := s.loadPolicyPacks(ctx)
	if err != nil {
		return err
	}
	mine := map[string]bool{}
	for _, id := range in.PolicyPacks {
		mine[id] = true
	}
	offered := map[string]guardrail.Pack{}
	for _, pk := range pkg.PolicyPacks {
		offered[pk.ID] = pk
	}
	var next []guardrail.Pack
	for _, cur := range set.Packs {
		pk, ok := offered[cur.ID]
		// libraryConflicts has refused a pack id someone else uses, and too many packs.
		switch {
		case ok:
			pk.Enabled = cur.Enabled
			next = append(next, pk)
			delete(offered, cur.ID)
		case mine[cur.ID]:
			// The new version no longer has it.
		default:
			next = append(next, cur)
		}
	}
	for _, pk := range pkg.PolicyPacks {
		if _, left := offered[pk.ID]; left {
			pk.Enabled = false
			next = append(next, pk)
		}
	}
	if next == nil {
		next = []guardrail.Pack{}
	}
	if err := s.docs.Put(ctx, kindSettings, idPolicyPacks, policyPacks{Packs: next, UpdatedAt: now, UpdatedBy: by}); err != nil {
		return err
	}
	in.PolicyPacks = nil
	for _, pk := range pkg.PolicyPacks {
		in.PolicyPacks = append(in.PolicyPacks, pk.ID)
	}
	return nil
}

// installMCPServers creates the package's MCP servers with no token and no tools turned on, or updates the ones an
// earlier install made. When an update changes where a server is (its address or image), its tools are forgotten:
// they must be discovered and turned on again.
func (s *Server) installMCPServers(ctx context.Context, in *MarketInstall, pkg MarketPackage, by string, now time.Time) error {
	if in.MCPServers == nil {
		in.MCPServers = map[string]string{}
	}
	all, err := s.listMCPServers(ctx)
	if err != nil {
		return err
	}
	byID := map[string]MCPServer{}
	for _, srv := range all {
		byID[srv.ID] = srv
	}
	keep := map[string]bool{}
	for _, m := range pkg.MCPServers {
		id, mine := in.MCPServers[m.Key]
		if !mine {
			id = m.Key
		}
		keep[m.Key] = true
		cur, exists := byID[id]
		if exists && !mine {
			return &installRefusal{fmt.Sprintf("an MCP server %q already exists; rename or remove it first", id)}
		}
		if !exists && len(byID) >= maxMCPServers {
			return &installRefusal{fmt.Sprintf("at most %d MCP servers", maxMCPServers)}
		}
		srv := cur
		if !exists {
			srv = MCPServer{ID: id, Tools: []MCPServerTool{}}
		}
		moved := srv.Kind != m.Kind || srv.URL != m.URL || srv.Image != m.Image
		srv.Name, srv.Kind, srv.URL, srv.Image, srv.Network, srv.Hosts = m.Name, m.Kind, m.URL, m.Image, m.Network, m.Hosts
		srv.AuthHeader, srv.AuthScheme = m.AuthHeader, m.AuthScheme
		srv.AllowPrivate, srv.ImageUser = false, false
		if moved {
			srv.Tools, srv.DiscoveredAt, srv.Protocol = []MCPServerTool{}, nil, ""
		}
		srv.UpdatedBy, srv.UpdatedAt = by, now
		if err := s.docs.Put(ctx, kindMCPServer, srv.ID, srv); err != nil {
			return err
		}
		byID[srv.ID] = srv
		in.MCPServers[m.Key] = srv.ID
	}
	for key, id := range in.MCPServers {
		if !keep[key] {
			if err := s.docs.Delete(ctx, kindMCPServer, id); err != nil && !errors.Is(err, docstore.ErrNotFound) {
				return err
			}
			delete(in.MCPServers, key)
		}
	}
	return nil
}

// uninstallLibrary removes the policy packs and MCP servers an install added.
func (s *Server) uninstallLibrary(ctx context.Context, in MarketInstall, by string) error {
	if len(in.PolicyPacks) > 0 {
		set, err := s.loadPolicyPacks(ctx)
		if err != nil {
			return err
		}
		drop := map[string]bool{}
		for _, id := range in.PolicyPacks {
			drop[id] = true
		}
		next := []guardrail.Pack{}
		for _, pk := range set.Packs {
			if !drop[pk.ID] {
				next = append(next, pk)
			}
		}
		if len(next) != len(set.Packs) {
			if err := s.docs.Put(ctx, kindSettings, idPolicyPacks, policyPacks{Packs: next, UpdatedAt: time.Now().UTC(), UpdatedBy: by}); err != nil {
				return err
			}
		}
	}
	ids := make([]string, 0, len(in.MCPServers))
	for _, id := range in.MCPServers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := s.docs.Delete(ctx, kindMCPServer, id); err != nil && !errors.Is(err, docstore.ErrNotFound) {
			return err
		}
	}
	return nil
}

// installRefusal is a reason an install cannot go ahead that the administrator can fix (409), not a server fault.
type installRefusal struct{ msg string }

func (e *installRefusal) Error() string { return e.msg }
