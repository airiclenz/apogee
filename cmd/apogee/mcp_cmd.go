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

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/mcp"
	"github.com/airiclenz/apogee/internal/mcpauth"
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
// `apogee mcp login|logout <name>`
// ----------------------------------------------------------------------------

// newMCPCommand builds `apogee mcp` — the OAuth login and logout of the configured MCP servers —
// over this process's own terminal and authorization-server client.
func newMCPCommand() *cobra.Command {
	return newMCPCommandWith(mcpLoginDeps{})
}

// newMCPCommandWith is newMCPCommand over the login dependencies the caller states. The bare noun
// does nothing on its own and prints its help, and an unknown word after it is refused; `login`
// and `logout` are the two verbs.
func newMCPCommandWith(deps mcpLoginDeps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Log in to and out of OAuth-protected MCP servers",
		Long: "apogee mcp manages the OAuth login of the MCP servers config.yaml configures\n" +
			"with `auth: oauth`. `apogee mcp login <name>` runs the browser login and saves\n" +
			"the token under ~/.apogee/mcp-auth; `apogee mcp logout <name>` deletes it. A\n" +
			"session refreshes a saved token on its own, and asks for a login at startup\n" +
			"when a server has none.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(mcpLoginCommand(deps), mcpLogoutCommand())
	return cmd
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
			server, target, err := resolveOAuthServer(cmd, &opts, args[0])
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

// addMCPConfigFlag declares the one flag both verbs take: the apogee home whose config.yaml names
// the server and whose token store holds its record.
func addMCPConfigFlag(cmd *cobra.Command, opts *config.Options) {
	cmd.Flags().StringVar(&opts.ConfigDir, "config", "",
		"apogee home directory for config/library/sessions (default: ~/.apogee)")
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
	notify := func(msg string) { cmd.PrintErrln(msg) }
	if err := config.ApplyConfig(opts, cmd.Flags().Changed, os.Getenv, os.ReadFile, notify); err != nil {
		return mcp.ServerConfig{}, mcpLoginTarget{}, err
	}
	roots, err := resolveRoots(opts.ConfigDir, opts.Workspace)
	if err != nil {
		return mcp.ServerConfig{}, mcpLoginTarget{}, err
	}

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
