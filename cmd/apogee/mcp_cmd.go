package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/mcpauth"
	"github.com/airiclenz/apogee/internal/sanitize"
	"github.com/airiclenz/apogee/internal/security"
)

// mcpLoginDeps is what an MCP OAuth login takes from its host rather than deciding for itself: the
// terminal it talks through, whether stdin is a terminal a startup may ask on, and the constructor
// of the client it speaks to authorization servers over. A driven run injects them — through
// rootDeps for the startup login, through newMCPCommandWith for `apogee mcp` — rather than
// swapping a seam under the whole package. The zero value is the production one.
type mcpLoginDeps struct {
	// terminal is the login's terminal (item 6's fetcher); its zero fields are this process's own.
	// Its workspace is filled per login from the resolved roots.
	terminal loginTerminal
	// isTerminal reports whether stdin is a terminal a startup may ask on. Nil checks os.Stdin.
	isTerminal func() bool
	// newAuthClient builds the authorization-server client, from the connect's guard and proxy.
	// Nil means mcpauth.NewAuthClient, which refuses a loopback or LAN authorization server.
	newAuthClient func(guard security.URLGuard, proxy func(*http.Request) (*url.URL, error)) *http.Client
}

// withDefaults fills every unset field with this process's own terminal and client.
func (d mcpLoginDeps) withDefaults() mcpLoginDeps {
	d.terminal = d.terminal.withDefaults()
	if d.isTerminal == nil {
		d.isTerminal = func() bool { return term.IsTerminal(os.Stdin.Fd()) }
	}
	if d.newAuthClient == nil {
		d.newAuthClient = mcpauth.NewAuthClient
	}
	return d
}

// mcpHost is the MCP Host a connect or a login runs in for the apogee home: liveMCPHost, with the
// authorization-server client the deps name.
func (d mcpLoginDeps) mcpHost(home string) mcp.Host {
	host := liveMCPHost(home)
	if d.newAuthClient != nil {
		host.NewOAuthClient = d.newAuthClient
	}
	return host
}

// mcpLoginTarget is where one login lands and what fences it: the apogee home whose token store
// receives the record, the workspace the browser opener may not resolve inside, and the
// operator's url-safety guard every endpoint and authorization-server request is judged by.
type mcpLoginTarget struct {
	home      string
	workspace string
	guard     security.URLGuard
}

// loginMCPServer runs one interactive OAuth login for server and saves the record under the
// target's home: the endpoint's vetted client carries the probe, the guarded authorization-server
// client everything else, and the terminal fetcher — bound first, so its redirect URL is known
// before a client is registered — obtains the code. It never touches a session.
func loginMCPServer(ctx context.Context, deps mcpLoginDeps, target mcpLoginTarget, server mcp.ServerConfig) error {
	deps = deps.withDefaults()
	host := deps.mcpHost(target.home)
	endpoint, endpointClient, err := mcp.EndpointClient(ctx, host, server, target.guard)
	if err != nil {
		return err
	}
	secret, err := mcpClientSecret(server)
	if err != nil {
		return err
	}

	terminal := deps.terminal
	terminal.workspace = target.workspace
	redirectURL, fetch, closeFetcher, err := newLoginFetcher(terminal)
	if err != nil {
		return fmt.Errorf("apogee: log in to MCP server %q: %w", server.Name, err)
	}
	defer closeFetcher()

	_, err = mcpauth.Login(ctx, mcpauth.LoginConfig{
		Name:           server.Name,
		Endpoint:       endpoint,
		Store:          host.OAuthStore,
		EndpointClient: endpointClient,
		AuthClient:     deps.newAuthClient(target.guard, host.Proxy),
		ClientID:       server.ClientID,
		ClientSecret:   secret,
		RedirectURL:    redirectURL,
		FetchCode:      fetch,
	})
	return err
}

// mcpClientSecret is a preregistered client's secret, read from its `client-secret-env:` variable
// at login time so it never sits in the config file. A server naming no variable has none; a
// variable that is not set is refused, naming the server and the variable but never a value.
func mcpClientSecret(server mcp.ServerConfig) (string, error) {
	if server.ClientSecretEnv == "" {
		return "", nil
	}
	secret, isSet := os.LookupEnv(server.ClientSecretEnv)
	if !isSet {
		return "", fmt.Errorf("apogee: MCP server %q: client-secret-env: the environment variable %s is not set",
			server.Name, server.ClientSecretEnv)
	}
	return secret, nil
}

// ----------------------------------------------------------------------------
// `apogee mcp list|login|logout`
// ----------------------------------------------------------------------------

// newMCPCommand builds `apogee mcp` — the list of the configured MCP servers and the OAuth login
// and logout of each — over this process's own terminal and authorization-server client.
func newMCPCommand() *cobra.Command {
	return newMCPCommandWith(mcpLoginDeps{})
}

// newMCPCommandWith is newMCPCommand over the login dependencies the caller states. The bare noun
// does nothing on its own and prints its help, and an unknown word after it is refused; `list`,
// `login` and `logout` are the three verbs.
func newMCPCommandWith(deps mcpLoginDeps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "List MCP servers, and log in to and out of OAuth-protected ones",
		Long: "apogee mcp shows and manages the MCP servers config.yaml configures.\n" +
			"`apogee mcp list` prints each one with where it comes from — your global config or\n" +
			"the project's .apogee/config.yaml — and, for a project one, whether it is adopted,\n" +
			"proposed or rejected. `apogee mcp login <name>` runs the browser login of an\n" +
			"`auth: oauth` server and saves the token under ~/.apogee/mcp-auth; `apogee mcp\n" +
			"logout <name>` deletes it. A session refreshes a saved token on its own, and asks\n" +
			"for a login at startup when a server has none.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(mcpListCommand(), mcpLoginCommand(deps), mcpLogoutCommand())
	return cmd
}

// mcpListCommand builds `apogee mcp list`: every MCP server the layered config names — the global
// ones, the project ones adopted on this machine, and the project ones still proposed or rejected,
// which run nothing — one row each with its transport, source and adoption state.
func mcpListCommand() *cobra.Command {
	var opts config.Options
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the configured MCP servers, their source and adoption state",
		Long: "apogee mcp list prints every MCP server the configuration names: its name, its\n" +
			"transport, its source — `global` for ~/.apogee/config.yaml, `project` for the\n" +
			"Project config — and, for a project server, `adopted`, `proposed` or `rejected`.\n" +
			"Only an adopted project server is connected; `apogee project adopt` answers the\n" +
			"proposed ones. It connects to nothing.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listMCPServers(cmd, &opts)
		},
	}
	addMCPConfigFlag(cmd, &opts)
	addMCPWorkspaceFlag(cmd, &opts)
	return cmd
}

// mcpServerRow is one row of `apogee mcp list`: a server as the list names it.
type mcpServerRow struct {
	name      string
	transport string
	source    string
	state     string
}

// Where an `apogee mcp list` row's server comes from, and the state column of a global one, which
// no adoption answer governs.
const (
	mcpSourceGlobal  = "global"
	mcpSourceProject = "project"
	mcpStateNone     = "-"
)

// listMCPServers resolves the config the way a session does and prints its servers. A config that
// names no startup LLM server still lists its MCP servers, as `apogee project adopt` still asks.
func listMCPServers(cmd *cobra.Command, opts *config.Options) error {
	notify := func(msg string) { cmd.PrintErrln(msg) }
	err := config.ApplyConfig(opts, cmd.Flags().Changed, os.Getenv, os.ReadFile, notify)
	var undetermined *config.StartupUndetermined
	if err != nil && !errors.As(err, &undetermined) {
		return err
	}
	rows := mcpServerRows(*opts)
	out := cmd.OutOrStdout()
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(out, "No MCP servers are configured.")
		return nil
	}
	table := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(table, "NAME\tTRANSPORT\tSOURCE\tSTATE")
	for _, row := range rows {
		_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", row.name, row.transport, row.source, row.state)
	}
	return table.Flush()
}

// mcpServerRows is every server of opts as `apogee mcp list` prints it: the configured servers in
// their order — the global ones, then the adopted project ones — then the project ones proposed,
// then those rejected. A project server's name and transport are the repository's text, so each
// is stripped to one line before it reaches the terminal.
func mcpServerRows(opts config.Options) []mcpServerRow {
	var rows []mcpServerRow
	for _, server := range opts.MCPServers {
		row := mcpServerRow{
			name: server.Name, transport: string(server.Transport), source: mcpSourceGlobal, state: mcpStateNone,
		}
		if server.Project {
			row.source, row.state = mcpSourceProject, "adopted"
		}
		rows = append(rows, row.stripped())
	}
	rows = appendProposedServerRows(rows, opts.Proposals.Proposed, "proposed")
	return appendProposedServerRows(rows, opts.Proposals.Rejected, "rejected")
}

// appendProposedServerRows appends to rows a project row in state for each repo server among
// proposals.
func appendProposedServerRows(rows []mcpServerRow, proposals []domain.Proposal, state string) []mcpServerRow {
	for _, p := range repoServerProposals(proposals) {
		rows = append(rows, mcpServerRow{
			name: p.Handle, transport: proposalTransport(p), source: mcpSourceProject, state: state,
		}.stripped())
	}
	return rows
}

// stripped is the row with its name and transport made safe to print on one line.
func (r mcpServerRow) stripped() mcpServerRow {
	r.name = sanitize.StripEscapesToLine(r.name)
	r.transport = sanitize.StripEscapesToLine(r.transport)
	return r
}

// repoServerProposals is the repo MCP servers among proposals, in order — the reactions left out.
func repoServerProposals(proposals []domain.Proposal) []domain.Proposal {
	var servers []domain.Proposal
	for _, p := range proposals {
		if p.Kind == domain.ProposalMCPServer {
			servers = append(servers, p)
		}
	}
	return servers
}

// proposalTransport is the `transport:` a repo server's proposal pins, or mcpStateNone when it
// pins none.
func proposalTransport(p domain.Proposal) string {
	for _, field := range p.Fields {
		if field.Label == "transport" && len(field.Values) > 0 {
			return field.Values[0]
		}
	}
	return mcpStateNone
}

// mcpLoginCommand builds `apogee mcp login <name>`: the interactive login of one configured
// `auth: oauth` server, which replaces any token saved for it.
func mcpLoginCommand(deps mcpLoginDeps) *cobra.Command {
	var opts config.Options
	cmd := &cobra.Command{
		Use:   "login <name>",
		Short: "Log in to an OAuth-protected MCP server and save its token",
		Long: "apogee mcp login prints the authorization server's login URL — Enter opens it on\n" +
			"a local desktop — and waits for the browser to come back to a loopback address,\n" +
			"or for the final redirect URL to be pasted. The token is saved under\n" +
			"~/.apogee/mcp-auth and refreshed by every session from then on.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			roots, err := loadMCPConfig(cmd, &opts)
			if err != nil {
				return err
			}
			if err := refuseUnadoptedRepoServer(opts, roots, args[0]); err != nil {
				return err
			}
			server, target, err := oauthServer(opts, roots, args[0])
			if err != nil {
				return err
			}
			if err := loginMCPServer(cmd.Context(), deps, target, server); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Logged in to MCP server %q.\n", server.Name)
			return nil
		},
	}
	addMCPConfigFlag(cmd, &opts)
	addMCPWorkspaceFlag(cmd, &opts)
	return cmd
}

// mcpLogoutCommand builds `apogee mcp logout <name>`: it deletes the token saved for one
// configured `auth: oauth` server and says whether there was one. It makes no network call — the
// token is not revoked at the authorization server.
func mcpLogoutCommand() *cobra.Command {
	var opts config.Options
	cmd := &cobra.Command{
		Use:           "logout <name>",
		Short:         "Delete the saved OAuth token of an MCP server",
		Long:          "apogee mcp logout deletes the token apogee saved for the server. It makes no\nnetwork call: the token is forgotten here, not revoked at the authorization server.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			server, target, err := resolveOAuthServer(cmd, &opts, args[0])
			if err != nil {
				return err
			}
			return logoutMCPServer(cmd.OutOrStdout(), mcpauth.NewStore(target.home), server)
		},
	}
	addMCPConfigFlag(cmd, &opts)
	return cmd
}

// addMCPConfigFlag declares the flag every verb takes: the apogee home whose config.yaml names
// the server and whose token store holds its record.
func addMCPConfigFlag(cmd *cobra.Command, opts *config.Options) {
	cmd.Flags().StringVar(&opts.ConfigDir, "config", "",
		"apogee home directory for config/library/sessions (default: ~/.apogee)")
}

// addMCPWorkspaceFlag declares the workspace whose Project config's servers `list` and `login`
// read. It is a flag of its own, not part of addMCPConfigFlag, which `apogee project adopt` also
// calls beside its own `--workspace`.
func addMCPWorkspaceFlag(cmd *cobra.Command, opts *config.Options) {
	cmd.Flags().StringVar(&opts.Workspace, "workspace", "",
		"workspace whose Project config's MCP servers to read (default: current directory)")
}

// logoutMCPServer deletes server's saved record and reports on out whether one existed. A record
// that could not be read is deleted all the same and reported as such.
func logoutMCPServer(out io.Writer, store *mcpauth.Store, server mcp.ServerConfig) error {
	_, isStored, loadErr := store.Load(server.Name, server.Endpoint)
	if err := store.Delete(server.Name); err != nil {
		return err
	}
	switch {
	case loadErr != nil:
		_, _ = fmt.Fprintf(out, "Logged out of MCP server %q: its unreadable token record was deleted.\n", server.Name)
	case isStored:
		_, _ = fmt.Fprintf(out, "Logged out of MCP server %q: its saved token was deleted.\n", server.Name)
	default:
		_, _ = fmt.Fprintf(out, "MCP server %q had no saved token; nothing to delete.\n", server.Name)
	}
	return nil
}

// resolveOAuthServer reads config.yaml the way a session does — flag over APOGEE_CONFIG over
// ~/.apogee — and returns the `auth: oauth` server it names with where its login lands. An unknown
// name and a server without `auth: oauth` are refused.
func resolveOAuthServer(cmd *cobra.Command, opts *config.Options, name string) (mcp.ServerConfig, mcpLoginTarget, error) {
	roots, err := loadMCPConfig(cmd, opts)
	if err != nil {
		return mcp.ServerConfig{}, mcpLoginTarget{}, err
	}
	return oauthServer(*opts, roots, name)
}

// loadMCPConfig resolves opts from the flags, the environment and the config files the way a
// session does, and returns the roots they name.
func loadMCPConfig(cmd *cobra.Command, opts *config.Options) (stateRoots, error) {
	notify := func(msg string) { cmd.PrintErrln(msg) }
	if err := config.ApplyConfig(opts, cmd.Flags().Changed, os.Getenv, os.ReadFile, notify); err != nil {
		return stateRoots{}, err
	}
	return resolveRoots(opts.ConfigDir, opts.Workspace)
}

// refuseUnadoptedRepoServer refuses a login to name when it is a repo server the user has not
// adopted (ADR 0104 §12): a proposed one points at `apogee project adopt`, which asks about it; a
// rejected one, which that command no longer asks about, at its row in /settings. Any other name
// passes.
func refuseUnadoptedRepoServer(opts config.Options, roots stateRoots, name string) error {
	path := projectConfigWatchPath(opts, roots.project)
	if hasRepoServer(opts.Proposals.Proposed, name) {
		return fmt.Errorf("apogee mcp: MCP server %q is proposed by the project config %s and not adopted "+
			"on this machine, so it has no login — run `apogee project adopt` to adopt it first", name, path)
	}
	if hasRepoServer(opts.Proposals.Rejected, name) {
		return fmt.Errorf("apogee mcp: MCP server %q from the project config %s was rejected on this machine, "+
			"so it has no login — adopt it from its `mcp-servers` row in /settings first", name, path)
	}
	return nil
}

// hasRepoServer reports whether proposals hold the repo server whose name is name.
func hasRepoServer(proposals []domain.Proposal, name string) bool {
	for _, p := range repoServerProposals(proposals) {
		if p.Handle == name {
			return true
		}
	}
	return false
}

// oauthServer returns the `auth: oauth` server of opts named name with where its login lands under
// roots. An unknown name and a server without `auth: oauth` are refused.
func oauthServer(opts config.Options, roots stateRoots, name string) (mcp.ServerConfig, mcpLoginTarget, error) {
	server, isFound := findMCPServer(opts.MCPServers, name)
	if !isFound {
		return mcp.ServerConfig{}, mcpLoginTarget{}, fmt.Errorf(
			"apogee mcp: no MCP server named %q in %s — the name is the `name:` of an `mcp-servers:` entry",
			name, config.FilePath(roots.config))
	}
	if server.Auth != mcp.AuthOAuth {
		return mcp.ServerConfig{}, mcpLoginTarget{}, fmt.Errorf(
			"apogee mcp: MCP server %q is not configured with auth: %s — there is no OAuth login to manage",
			name, mcp.AuthOAuth)
	}
	return server, mcpLoginTarget{
		home:      roots.config,
		workspace: roots.workspace,
		guard:     mcpGuard(opts.URLAllowHosts, opts.URLDenyHosts),
	}, nil
}

// findMCPServer returns the configured server whose alias is name.
func findMCPServer(servers []mcp.ServerConfig, name string) (mcp.ServerConfig, bool) {
	for _, server := range servers {
		if server.Name == name {
			return server, true
		}
	}
	return mcp.ServerConfig{}, false
}

// ----------------------------------------------------------------------------
// The startup login
// ----------------------------------------------------------------------------

// connectMCPServers is the session's startup connect with the login a fresh `auth: oauth` server
// needs. Before the connect, every such server with no saved token is offered a login; after it,
// a connect that still fails for want of a login (a refresh the authorization server rejected)
// offers that server one and connects again — at most once per `auth: oauth` server, so the loop
// always ends. Each offer asks on stderr while it is still a safe place to write: declined, or
// with stdin not a terminal, the launch aborts with the error naming `apogee mcp login <name>`.
func (w *rootWiring) connectMCPServers(ctx context.Context) (*mcp.Client, error) {
	deps := w.mcpLogin.withDefaults()
	host := deps.mcpHost(w.roots.config)
	guard := mcpGuard(w.cfg.URLAllowHosts, w.cfg.URLDenyHosts)
	login := mcpStartupLogin{
		deps:   deps,
		target: mcpLoginTarget{home: w.roots.config, workspace: w.roots.workspace, guard: guard},
	}

	retries := 0
	for _, server := range w.opts.MCPServers {
		if server.Auth != mcp.AuthOAuth {
			continue
		}
		retries++
		if err := login.offerUnsaved(ctx, host.OAuthStore, server); err != nil {
			return nil, err
		}
	}

	for {
		client, err := mcp.ConnectWith(ctx, host, w.opts.MCPServers, guard, w.roots.workspace)
		var required *mcpauth.LoginRequiredError
		if err == nil || retries == 0 || !errors.As(err, &required) {
			return client, err
		}
		retries--
		server, isFound := findMCPServer(w.opts.MCPServers, required.Server)
		if !isFound {
			return nil, err
		}
		if err := login.offer(ctx, server, err); err != nil {
			return nil, err
		}
	}
}

// mcpStartupLogin is the startup's login offer over one session's home, workspace and guard.
type mcpStartupLogin struct {
	deps   mcpLoginDeps
	target mcpLoginTarget
}

// offerUnsaved offers server a login when the store holds no token for its endpoint. A record that
// cannot be read is left to the connect, which reports it naming the server.
func (l mcpStartupLogin) offerUnsaved(ctx context.Context, store *mcpauth.Store, server mcp.ServerConfig) error {
	_, isStored, err := store.Load(server.Name, server.Endpoint)
	if err != nil || isStored {
		return nil
	}
	return l.offer(ctx, server, &mcpauth.LoginRequiredError{Server: server.Name, Reason: "no stored token for this endpoint"})
}

// offer asks whether to log in to server now and runs the login on a yes. required is what the
// launch aborts with otherwise: stdin not a terminal, the answer unreadable, or anything but yes.
func (l mcpStartupLogin) offer(ctx context.Context, server mcp.ServerConfig, required error) error {
	if !l.deps.isTerminal() {
		return required
	}
	out := l.deps.terminal.out
	_, _ = fmt.Fprintf(out, "MCP server %q needs an OAuth login. Log in now? [y/N] ", server.Name)
	answer, err := readAnswer(ctx, l.deps.terminal)
	if err != nil {
		_, _ = fmt.Fprintln(out)
		return required
	}
	if !isYes(answer) {
		return required
	}
	if err := loginMCPServer(ctx, l.deps, l.target, server); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "Logged in to MCP server %q.\n", server.Name)
	return nil
}

// isYes reports whether answer is a yes: `y` or `yes` in any case. Anything else, an empty line
// above all, is the prompt's default no.
func isYes(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	}
	return false
}

// answerLine is one read of the terminal: the line, or why there is none.
type answerLine struct {
	text string
	err  error
}

// readAnswer reads one line from the terminal through a cancellable reader of its own, which it
// cancels, joins and closes before returning, so the login fetch that may follow opens the
// terminal afresh. ctx's end abandons the read.
func readAnswer(ctx context.Context, terminal loginTerminal) (string, error) {
	reader, err := terminal.openInput()
	if err != nil {
		return "", err
	}
	answers := make(chan answerLine, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(reader)
		scanner.Split(splitTerminalLines())
		if scanner.Scan() {
			answers <- answerLine{text: scanner.Text()}
			return
		}
		answers <- answerLine{err: errors.Join(io.EOF, scanner.Err())}
	}()
	defer func() {
		// A reader that cannot interrupt a blocked read (cancelreader's fallback) is not joined,
		// as in the fetch; Close releases only its cancel plumbing.
		if reader.Cancel() {
			<-done
		}
		_ = reader.Close()
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case answer := <-answers:
		return answer.text, answer.err
	}
}
