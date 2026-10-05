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
	"workctl/internal/secret"
)

// stdin is where the wizards read answers and piped tokens from; tests replace it.
var stdin io.Reader = os.Stdin

// endpointSpec describes what differs between the modules that share the endpoint wizard.
type endpointSpec struct {
	label         string
	auths         []string
	tokenPrompt   string
	tokenHelp     string
	defaultURL    string
	listPrompt    string // repositories (github) or projects (gitlab); empty for jira
	defaultPrompt string
	// verify makes one authenticated request and says who we are.
	verify func(ctx context.Context, c *APIClient) (string, error)
}

var endpointSpecs = map[string]endpointSpec{
	"jira": {
		label: "Jira", auths: []string{"bearer", "basic"},
		tokenPrompt: "Personal access token (password for basic auth)",
		tokenHelp:   "Create a personal access token in Jira under Profile > Personal Access Tokens.",
		verify: func(ctx context.Context, c *APIClient) (string, error) {
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
	},
	"gitlab": {
		label: "GitLab", auths: []string{"private-token", "bearer"},
		tokenPrompt:   "Access token",
		tokenHelp:     "Create an access token under User settings > Access tokens (scope read_api; api only if you allow writes).",
		listPrompt:    "Projects to search (group/project, comma separated)",
		defaultPrompt: "Default project",
		verify: func(ctx context.Context, c *APIClient) (string, error) {
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
	},
	"github": {
		label: "GitHub", auths: []string{"bearer"},
		defaultURL:    "https://api.github.com",
		tokenPrompt:   "Access token",
		tokenHelp:     "Create a fine-grained token under Settings > Developer settings (read access to contents, pull requests, actions and checks; write only if you allow writes).",
		listPrompt:    "Repositories to search (owner/name, comma separated)",
		defaultPrompt: "Default repository",
		verify: func(ctx context.Context, c *APIClient) (string, error) {
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
	},
}

func firstNonEmpty(items ...string) string {
	for _, s := range items {
		if s != "" {
			return s
		}
	}
	return ""
}

// cmdEndpointInit is `workctl jira|gitlab|github init`: write the profile (never the token), keep the token in the
// keyring and check the connection.
func cmdEndpointInit(path, module string, args []string, r *bufio.Reader) error {
	s, ok := endpointSpecs[module]
	if !ok {
		return fmt.Errorf("no setup for module %q", module)
	}
	fs := flag.NewFlagSet(module+" init", flag.ContinueOnError)
	profileFlag := fs.String("profile", "", "profile name (default: defaults."+module+", else "+module+")")
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
	cfg, err := loadConfigOrDefault(path)
	if err != nil {
		return err
	}
	name := firstNonEmpty(*profileFlag, cfg.Defaults[module], module)
	p, ok := cfg.Profiles[name]
	if !ok {
		p = Profile{Kind: module, URL: s.defaultURL, Auth: s.auths[0], TokenEnv: "WORKCTL_" + strings.ToUpper(module) + "_TOKEN", VerifyTLS: true}
	}
	if p.Kind != module {
		return fmt.Errorf("profile %q is %s, not %s", name, p.Kind, module)
	}
	if *urlFlag != "" {
		p.URL = *urlFlag
	}
	if *authFlag != "" {
		p.Auth = *authFlag
	}
	if *user != "" {
		p.Username = *user
	}
	if *ca != "" {
		p.CAFile = *ca
	}
	if *insecure {
		p.VerifyTLS = false
	}
	list, def := &p.Projects, &p.DefaultProject
	if module == "github" {
		list, def = &p.Repos, &p.DefaultRepo
	}
	if s.listPrompt != "" {
		if *projects != "" {
			*list = parseCommaList(*projects)
		}
		if *defProject != "" {
			*def = *defProject
		}
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
		fmt.Printf("\n%s (profile %s)\n", s.label, name)
		p.URL = prompt.Ask(r, "Base URL", p.URL)
		if len(s.auths) > 1 {
			p.Auth = prompt.AskChoice(r, "Authentication", s.auths, firstNonEmpty(p.Auth, s.auths[0]))
		}
		if strings.EqualFold(p.Auth, "basic") {
			p.Username = prompt.Ask(r, "Username", p.Username)
		}
		p.VerifyTLS = prompt.AskBool(r, "Verify TLS certificates", p.VerifyTLS)
		p.CAFile = prompt.Ask(r, "Custom CA bundle, PEM file (optional)", p.CAFile)
		if s.listPrompt != "" {
			*list = parseCommaList(prompt.Ask(r, s.listPrompt, strings.Join(*list, ",")))
			*def = prompt.Ask(r, s.defaultPrompt, *def)
		}
		fmt.Println(s.tokenHelp)
		fmt.Println("The token goes to the OS keyring; leave it empty to keep the one already stored.")
		if tok, err = prompt.AskSecret(r, s.tokenPrompt); err != nil {
			return err
		}
	}
	if normalizeBaseURL(p.URL) == "" {
		return fmt.Errorf("%s: a base URL is required", module)
	}
	if u, err := url.Parse(normalizeBaseURL(p.URL)); err == nil && u.Scheme == "http" {
		fmt.Fprintln(os.Stderr, "warning: http:// sends the credential unencrypted")
	}
	if s.listPrompt != "" && *def == "" && len(*list) > 0 {
		*def = (*list)[0]
	}
	cfg.Profiles[name] = p
	if cfg.Defaults[module] == "" {
		cfg.Defaults[module] = name
	}
	if err := validateConfig(cfg); err != nil {
		return err
	}

	var tokErr error
	if tok != "" {
		if tokErr = tokenStore.Set(secret.Account(module, name), tok); tokErr != nil {
			tokErr = fmt.Errorf("%w\nuse the environment variable %s instead", tokErr, p.TokenEnv)
		}
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
	c, err := newProfileClient(cfg, name, p)
	who := ""
	if err == nil {
		who, err = s.verify(ctx, c)
	}
	if err != nil {
		fmt.Printf("connection test FAILED: %v\n", err)
		return nil
	}
	fmt.Println(who)
	return nil
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
	cfg, err := loadConfigOrDefault(path)
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
