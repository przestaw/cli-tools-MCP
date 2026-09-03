package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"workctl/internal/prompt"
)

// stdin is where the wizards read answers and piped tokens from; tests replace it.
var stdin io.Reader = os.Stdin

// endpointSpec describes what differs between the modules that share the endpoint wizard (Jira, GitLab).
type endpointSpec struct {
	module      string
	label       string
	auths       []string
	tokenPrompt string
	tokenHelp   string
	ec          *EndpointConfig
	defaultURL  string
	// extra asks for module-specific settings; it is not called with --non-interactive.
	extra func(r *bufio.Reader)
	// verify makes one authenticated request and says who we are.
	verify func(ctx context.Context, cfg *Config) (string, error)
}

func endpointSpecFor(cfg *Config, module string) (*endpointSpec, error) {
	switch module {
	case "jira":
		return &endpointSpec{
			module: "jira", label: "Jira", auths: []string{"bearer", "basic"},
			tokenPrompt: "Personal access token (password for basic auth)",
			tokenHelp:   "Create a personal access token in Jira under Profile > Personal Access Tokens.",
			ec:          &cfg.Jira,
			verify: func(ctx context.Context, cfg *Config) (string, error) {
				c, err := newJiraClient(cfg)
				if err != nil {
					return "", err
				}
				b, err := jiraMyself(ctx, c)
				if err != nil {
					return "", err
				}
				var me struct {
					DisplayName string `json:"displayName"`
					Name        string `json:"name"`
				}
				_ = json.Unmarshal(b, &me)
				return "connected as " + firstNonEmpty(me.DisplayName, me.Name, "(unknown user)"), nil
			},
		}, nil
	case "gitlab":
		s := &endpointSpec{
			module: "gitlab", label: "GitLab", auths: []string{"private-token", "bearer"},
			tokenPrompt: "Access token",
			tokenHelp:   "Create an access token under User settings > Access tokens (scope read_api; api only if you allow writes).",
			ec:          &cfg.GitLab.EndpointConfig,
			verify: func(ctx context.Context, cfg *Config) (string, error) {
				c, err := newGitLabClient(cfg)
				if err != nil {
					return "", err
				}
				b, err := gitlabVersion(ctx, c)
				if err != nil {
					return "", err
				}
				var v struct {
					Version string `json:"version"`
				}
				_ = json.Unmarshal(b, &v)
				return "connected, GitLab " + firstNonEmpty(v.Version, "(unknown version)"), nil
			},
		}
		s.extra = func(r *bufio.Reader) {
			cfg.GitLab.Projects = parseCommaList(prompt.Ask(r, "Projects to search (group/project, comma separated)", strings.Join(cfg.GitLab.Projects, ",")))
			cfg.GitLab.DefaultProject = prompt.Ask(r, "Default project", cfg.GitLab.DefaultProject)
		}
		return s, nil
	case "github":
		s := &endpointSpec{
			module: "github", label: "GitHub", auths: []string{"bearer"},
			defaultURL:  "https://api.github.com",
			tokenPrompt: "Access token",
			tokenHelp:   "Create a fine-grained token under Settings > Developer settings (read access to contents, pull requests, actions and checks; write only if you allow writes).",
			ec:          &cfg.GitHub.EndpointConfig,
			verify: func(ctx context.Context, cfg *Config) (string, error) {
				c, err := newGitHubClient(cfg)
				if err != nil {
					return "", err
				}
				b, err := githubUser(ctx, c)
				if err != nil {
					return "", err
				}
				var u struct {
					Login string `json:"login"`
				}
				_ = json.Unmarshal(b, &u)
				return "connected as " + firstNonEmpty(u.Login, "(unknown user)"), nil
			},
		}
		s.extra = func(r *bufio.Reader) {
			cfg.GitHub.Repos = parseCommaList(prompt.Ask(r, "Repositories to search (owner/name, comma separated)", strings.Join(cfg.GitHub.Repos, ",")))
			cfg.GitHub.DefaultRepo = prompt.Ask(r, "Default repository", cfg.GitHub.DefaultRepo)
		}
		return s, nil
	}
	return nil, fmt.Errorf("no setup for module %q", module)
}

func firstNonEmpty(items ...string) string {
	for _, s := range items {
		if s != "" {
			return s
		}
	}
	return ""
}

// storeToken puts a credential in the OS keyring and drops any plaintext copy from the config.
func storeToken(s *endpointSpec, tok string) error {
	if err := tokenStore.Set(keyringAccount(s.module, *s.ec), tok); err != nil {
		return fmt.Errorf("%w\nuse the environment variable %s instead", err, s.ec.TokenEnv)
	}
	s.ec.Token = ""
	return nil
}

// cmdEndpointInit is `workctl jira init` and `workctl gitlab init`: ask for the connection, keep the token in the
// keyring, write the config (never the token) and check the connection.
func cmdEndpointInit(path, module string, args []string, r *bufio.Reader) error {
	fs := flag.NewFlagSet(module+" init", flag.ContinueOnError)
	urlFlag := fs.String("url", "", "base URL")
	authFlag := fs.String("auth", "", "authentication scheme")
	user := fs.String("user", "", "username (basic auth)")
	ca := fs.String("ca", "", "custom PEM CA bundle")
	insecure := fs.Bool("insecure", false, "do not verify TLS certificates")
	tokenStdin := fs.Bool("token-stdin", false, "read the token from the first line of stdin")
	nonInteractive := fs.Bool("non-interactive", false, "do not prompt")
	projects := fs.String("projects", "", "comma-separated group/project (gitlab) or owner/name (github) paths")
	defProject := fs.String("default-project", "", "default group/project (gitlab) or owner/name (github)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfigForSetup(path)
	if err != nil {
		return err
	}
	s, err := endpointSpecFor(cfg, module)
	if err != nil {
		return err
	}
	ec := s.ec
	if *urlFlag != "" {
		ec.URL = *urlFlag
	}
	if *authFlag != "" {
		ec.Auth = *authFlag
	}
	if *user != "" {
		ec.Username = *user
	}
	if *ca != "" {
		ec.CAFile = *ca
	}
	if *insecure {
		ec.VerifyTLS = false
	}
	if module == "gitlab" {
		if *projects != "" {
			cfg.GitLab.Projects = parseCommaList(*projects)
		}
		if *defProject != "" {
			cfg.GitLab.DefaultProject = *defProject
		}
	}
	if module == "github" {
		if *projects != "" {
			cfg.GitHub.Repos = parseCommaList(*projects)
		}
		if *defProject != "" {
			cfg.GitHub.DefaultRepo = *defProject
		}
	}
	if ec.URL == "" {
		ec.URL = s.defaultURL
	}
	if r == nil {
		r = bufio.NewReader(stdin)
	}

	var tok string
	if *nonInteractive {
		if *tokenStdin {
			line, _ := r.ReadString('\n')
			tok = strings.TrimSpace(line)
		}
	} else {
		fmt.Printf("\n%s\n", s.label)
		ec.URL = prompt.Ask(r, "Base URL", ec.URL)
		ec.Auth = s.auths[0]
		if len(s.auths) > 1 {
			ec.Auth = prompt.AskChoice(r, "Authentication", s.auths, ec.Auth)
		}
		if strings.EqualFold(ec.Auth, "basic") {
			ec.Username = prompt.Ask(r, "Username", ec.Username)
		}
		ec.VerifyTLS = prompt.AskBool(r, "Verify TLS certificates", ec.VerifyTLS)
		ec.CAFile = prompt.Ask(r, "Custom CA bundle, PEM file (optional)", ec.CAFile)
		if s.extra != nil {
			s.extra(r)
		}
		fmt.Println(s.tokenHelp)
		fmt.Println("The token goes to the OS keyring; leave it empty to keep the one already stored.")
		if tok, err = prompt.AskSecret(r, s.tokenPrompt); err != nil {
			return err
		}
	}
	if normalizeBaseURL(ec.URL) == "" {
		return fmt.Errorf("%s: a base URL is required", module)
	}
	if u, err := url.Parse(normalizeBaseURL(ec.URL)); err == nil && u.Scheme == "http" {
		fmt.Fprintln(os.Stderr, "warning: http:// sends the credential unencrypted")
	}
	if module == "gitlab" && cfg.GitLab.DefaultProject == "" && len(cfg.GitLab.Projects) > 0 {
		cfg.GitLab.DefaultProject = cfg.GitLab.Projects[0]
	}
	if module == "github" && cfg.GitHub.DefaultRepo == "" && len(cfg.GitHub.Repos) > 0 {
		cfg.GitHub.DefaultRepo = cfg.GitHub.Repos[0]
	}
	if err := validateConfig(cfg); err != nil {
		return err
	}

	var tokErr error
	if tok != "" {
		tokErr = storeToken(s, tok)
	}
	if err := cfg.save(); err != nil {
		return err
	}
	fmt.Printf("saved: %s\n", cfg.path)
	if tokErr != nil {
		return tokErr
	}
	if tok != "" {
		fmt.Println("token stored in the OS keyring")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	who, err := s.verify(ctx, cfg)
	if err != nil {
		fmt.Printf("connection test FAILED: %v\n", err)
		return nil
	}
	fmt.Println(who)
	return nil
}

// cmdEndpointSetToken replaces the stored token of a configured module.
func cmdEndpointSetToken(path, module string, r *bufio.Reader) error {
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}
	s, err := endpointSpecFor(cfg, module)
	if err != nil {
		return err
	}
	if normalizeBaseURL(s.ec.URL) == "" {
		return fmt.Errorf("%s is not configured: run `workctl %s init`", module, module)
	}
	if r == nil {
		r = bufio.NewReader(stdin)
	}
	tok, err := prompt.AskSecret(r, s.tokenPrompt)
	if err != nil {
		return err
	}
	if tok == "" {
		return fmt.Errorf("empty token, nothing stored")
	}
	if err := storeToken(s, tok); err != nil {
		return err
	}
	fmt.Println("token stored in the OS keyring")
	return cfg.save()
}

var initModules = []struct{ name, label string }{
	{"jira", "Jira"},
	{"gitlab", "GitLab"},
	{"github", "GitHub"},
	{"azure", "Azure DevOps"},
	{"atlassian", "Jira and Confluence Cloud"},
}

// initModule runs the setup of one module.
func initModule(path, name string, r *bufio.Reader) error {
	switch name {
	case "jira", "gitlab", "github":
		return cmdEndpointInit(path, name, nil, r)
	case "azure", "atlassian":
		return cliModules[name].run([]string{"config", "init"})
	}
	return fmt.Errorf("unknown module %q (modules: jira, gitlab, github, azure, atlassian)", name)
}

// cmdInit is `workctl init [module...]`: with no module it walks through all of them.
func cmdInit(path string, args []string) error {
	r := bufio.NewReader(stdin)
	if len(args) > 0 {
		for _, a := range args {
			name := a
			if full := moduleAliases[a]; full != "" {
				name = full
			}
			if err := initModule(path, name, r); err != nil {
				return err
			}
		}
		return nil
	}
	fmt.Println("Each service is optional. Tokens are stored in the OS keyring, never in the config file.")
	for _, m := range initModules {
		if !prompt.AskBool(r, "Set up "+m.label+"?", false) {
			continue
		}
		if err := initModule(path, m.name, r); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", m.name, err)
		}
	}
	cfg, err := loadConfigForSetup(path)
	if err != nil {
		return err
	}
	cfg.Policy.AllowWrite = prompt.AskBool(r, "Allow write operations (comments, merge requests, ...)", cfg.Policy.AllowWrite)
	if err := cfg.save(); err != nil {
		return err
	}
	fmt.Printf("saved: %s\n", cfg.path)
	return nil
}
