// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/secret"
	"workctl/internal/testutil"
)

func TestCLIHelperProcess(t *testing.T) { testutil.CLIHelperProcess() }

func TestGitCredentialAnswersOnlyForItsHost(t *testing.T) {
	t.Setenv("TEST_GH_CRED", "tok")
	cfg := config.Default()
	cfg.Profiles["gh"] = config.Profile{Kind: "github", URL: "https://api.github.com", TokenEnv: "TEST_GH_CRED"}
	cfg.SetProfileName("gh")
	get := func(host string) string {
		var out strings.Builder
		if err := GitCredential(cfg, []string{"get"}, strings.NewReader("protocol=https\nhost="+host+"\n\n"), &out); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if got := get("github.com"); got != "username=x-access-token\npassword=tok\n" {
		t.Fatalf("github.com: %q", got)
	}
	if got := get("evil.example"); got != "" {
		t.Fatalf("other host got a credential: %q", got)
	}
	cfg.SetProfileName("")
	if got := get("github.com"); got != "username=x-access-token\npassword=tok\n" {
		t.Fatalf("a single profile on the host must answer without --profile, got %q", got)
	}
	if err := GitCredential(cfg, []string{"get"}, strings.NewReader(""), &strings.Builder{}); err != nil {
		t.Fatalf("an empty request must be ignored: %v", err)
	}
}

func TestGitCredentialPicksTheProfileByRepositoryPath(t *testing.T) {
	t.Setenv("TEST_PRZ", "tok-przestaw")
	t.Setenv("TEST_WORK", "tok-work")
	cfg := config.Default()
	cfg.Profiles["przestaw"] = config.Profile{Kind: "github", URL: "https://api.github.com", TokenEnv: "TEST_PRZ", Repos: []string{"przestaw/*"}}
	cfg.Profiles["work"] = config.Profile{Kind: "github", URL: "https://api.github.com", TokenEnv: "TEST_WORK", Repos: []string{"corp/*"}}
	get := func(path string) string {
		var out strings.Builder
		in := "protocol=https\nhost=github.com\n"
		if path != "" {
			in += "path=" + path + "\n"
		}
		if err := GitCredential(cfg, []string{"get"}, strings.NewReader(in+"\n"), &out); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if got := get("przestaw/cli-tools-MCP.git"); !strings.Contains(got, "password=tok-przestaw") {
		t.Fatalf("przestaw repo: %q", got)
	}
	if got := get("corp/service.git"); !strings.Contains(got, "password=tok-work") {
		t.Fatalf("corp repo: %q", got)
	}
	if got := get("stranger/repo.git"); got != "" {
		t.Fatalf("an unbound repository must get no credential: %q", got)
	}
	if got := get(""); got != "" {
		t.Fatalf("no path and no default must get no credential: %q", got)
	}
	cfg.Defaults["github"] = "work"
	if got := get("stranger/repo.git"); !strings.Contains(got, "password=tok-work") {
		t.Fatalf("defaults.github must answer for an unbound repository: %q", got)
	}
}

func TestGitCredentialAsksGhForACLIProfile(t *testing.T) {
	calls := testutil.FakeCLI(t, "token")
	cfg := config.Default()
	cfg.Profiles["gh"] = config.Profile{Kind: "github", URL: "https://api.github.com", Backend: config.BackendCLI, VerifyTLS: true, Username: "przestaw"}
	var out strings.Builder
	if err := GitCredential(cfg, []string{"get"}, strings.NewReader("protocol=https\nhost=github.com\n\n"), &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "username=x-access-token\npassword=gho_faketoken\n" {
		t.Fatalf("out = %q", out.String())
	}
	if got := strings.Join((*calls)[0], " "); got != "gh auth token --hostname github.com --user przestaw" {
		t.Fatalf("gh call = %q", got)
	}
}

func TestSetupGitDryRunListsTheConfigForEveryHost(t *testing.T) {
	cfg := config.Default()
	cfg.Profiles["a"] = config.Profile{Kind: "github", URL: "https://api.github.com"}
	cfg.Profiles["b"] = config.Profile{Kind: "gitlab", URL: "https://gitlab.example.com"}
	cfg.Profiles["j"] = config.Profile{Kind: "jira", URL: "https://jira.example.com"}
	calls := testutil.FakeCLI(t, "ok")
	if err := setupGitCommand(cfg, []string{"--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Fatalf("a dry run started git: %v", *calls)
	}
	if err := setupGitCommand(cfg, nil); err != nil {
		t.Fatal(err)
	}
	var sections []string
	for _, c := range *calls {
		if c[0] == "git" && c[len(c)-2] == "credential.https://github.com.useHttpPath" {
			sections = append(sections, "github")
		}
		if c[0] == "git" && c[len(c)-2] == "credential.https://gitlab.example.com.useHttpPath" {
			sections = append(sections, "gitlab")
		}
	}
	if strings.Join(sections, ",") != "github,gitlab" || len(*calls) != 8 {
		t.Fatalf("calls = %v", *calls)
	}
}

func TestAuthSetTokenAndStatusAndLogout(t *testing.T) {
	keyring.MockInit()
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{
		"gh": {Kind: "github", URL: "https://api.github.example"},
	}
	cfg.SetPath(cfgPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	old := cliutil.Stdin
	cliutil.Stdin = strings.NewReader("my-secret-token\n")
	defer func() { cliutil.Stdin = old }()

	if err := Command(cfgPath, "", []string{"set-token", "--profile", "gh", "--stdin"}); err != nil {
		t.Fatalf("set-token failed: %v", err)
	}

	account := secret.Account("github", "gh")
	val, err := config.TokenStore.Get(account)
	if err != nil || val != "my-secret-token" {
		t.Fatalf("token in keyring = %q, %v", val, err)
	}

	if err := Command(cfgPath, "", []string{"status", "--profile", "gh"}); err != nil {
		t.Fatalf("status failed: %v", err)
	}

	if err := Command(cfgPath, "", []string{"logout", "--profile", "gh"}); err != nil {
		t.Fatalf("logout failed: %v", err)
	}

	val, err = config.TokenStore.Get(account)
	if err == nil {
		t.Fatalf("expected token to be deleted, got %q", val)
	}
}

func TestAuthStatusWithEnvVar(t *testing.T) {
	keyring.MockInit()
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{
		"gl": {Kind: "gitlab", URL: "https://gitlab.example", TokenEnv: "TEST_GL_TOKEN_AUTH"},
	}
	cfg.SetPath(cfgPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TEST_GL_TOKEN_AUTH", "env-secret")
	if err := Command(cfgPath, "", []string{"status", "--profile", "gl"}); err != nil {
		t.Fatalf("status failed: %v", err)
	}
}

func TestAuthLoginBindsACLIProfileToItsAccount(t *testing.T) {
	testutil.FakeCLI(t, "user")
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{"gh": {Kind: "github", URL: "https://api.github.com", Backend: config.BackendCLI, VerifyTLS: true}}
	cfg.SetPath(cfgPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	if err := Command(cfgPath, "gh", []string{"login"}); err != nil { // the profile comes from the global --profile
		t.Fatal(err)
	}
	saved, err := config.Load(cfgPath)
	if err != nil || saved.Profiles["gh"].Username != "przestaw" {
		t.Fatalf("profile not bound: %+v, %v", saved.Profiles["gh"], err)
	}
	if err := Command(cfgPath, "", []string{"status"}); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"set-token", "logout"} {
		if err := Command(cfgPath, "gh", []string{sub}); err == nil || !strings.Contains(err.Error(), "auth login") {
			t.Errorf("%s on a cli profile: %v", sub, err)
		}
	}

	other := saved.Profiles["gh"]
	other.Username = "someone-else"
	saved.Profiles["gh"] = other
	if err := saved.Save(); err != nil {
		t.Fatal(err)
	}
	if err := Command(cfgPath, "gh", []string{"login"}); err == nil || !strings.Contains(err.Error(), "bound to") {
		t.Fatalf("login as another account: %v", err)
	}
	if again, _ := config.Load(cfgPath); again.Profiles["gh"].Username != "someone-else" {
		t.Fatal("a refused login changed the profile")
	}
}

func TestAccountsMCPToolListsResolvesAndBinds(t *testing.T) {
	keyring.MockInit()
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{
		"przestaw": {Kind: "github", URL: "https://api.github.com", Username: "przestaw", VerifyTLS: true},
		"work":     {Kind: "github", URL: "https://api.github.com", Username: "work-user", Repos: []string{"corp/*"}, VerifyTLS: true},
		"jira-ir":  {Kind: "jira", URL: "https://jira.example", Projects: []string{"IR"}, VerifyTLS: true},
	}
	cfg.Defaults["github"] = "work"
	cfg.Policy.AllowWrite = true
	cfg.SetPath(cfgPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := config.TokenStore.Set(secret.Account("github", "przestaw"), "super-secret-token"); err != nil {
		t.Fatal(err)
	}

	// Bind "przestaw/*" to profile "przestaw" and resolve "przestaw/cli-tools-MCP".
	out, err := CallTool(context.Background(), cfg, "accounts", map[string]any{
		"bind_profile": "przestaw",
		"bind_repo":    "przestaw/*",
		"resource":     "przestaw/cli-tools-MCP",
	})
	if err != nil {
		t.Fatalf("accounts bind: %v", err)
	}
	if strings.Contains(string(out), "super-secret-token") {
		t.Fatal("accounts output leaked the secret token")
	}
	var report AccountsReport
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatal(err)
	}
	if report.ResolvedProfile != "przestaw" {
		t.Fatalf("resolved_profile = %q, want przestaw", report.ResolvedProfile)
	}
	saved, err := config.Load(cfgPath)
	if err != nil || len(saved.Profiles["przestaw"].Repos) != 1 || saved.Profiles["przestaw"].Repos[0] != "przestaw/*" {
		t.Fatalf("saved repos = %+v, err = %v", saved.Profiles["przestaw"].Repos, err)
	}

	// Refuses binding a pattern already owned by another profile on the same host.
	if _, err := CallTool(context.Background(), cfg, "accounts", map[string]any{
		"bind_profile": "work",
		"bind_repo":    "przestaw/*",
	}); err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("expected conflict error, got %v", err)
	}

	// Resolves Jira issue key across kinds.
	out, err = CallTool(context.Background(), cfg, "accounts", map[string]any{"resource": "IR-101"})
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(out, &report)
	if report.ResolvedProfile != "jira-ir" {
		t.Fatalf("jira resolved_profile = %q, want jira-ir", report.ResolvedProfile)
	}
}

func TestBindNeedsAllowWriteAndKeepsTheFileChangedMeanwhile(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{"gh": {Kind: "github", URL: "https://api.github.com", VerifyTLS: true}}
	cfg.SetPath(cfgPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"bind_profile": "gh", "bind_repo": "o/*"}
	if _, err := CallTool(context.Background(), cfg, "accounts", args); err == nil || !strings.Contains(err.Error(), "allow_write") {
		t.Fatalf("bind with allow_write off: %v", err)
	}
	if saved, _ := config.Load(cfgPath); len(saved.Profiles["gh"].Repos) != 0 {
		t.Fatal("a refused bind changed the file")
	}

	// the owner turns allow_write on and adds a profile in the file after this process started
	cfg.Policy.AllowWrite = true
	edited, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	edited.Policy.AllowWrite = true
	edited.Profiles["extra"] = config.Profile{Kind: "gitlab", URL: "https://gitlab.example", VerifyTLS: true}
	if err := edited.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := CallTool(context.Background(), cfg, "accounts", args); err != nil {
		t.Fatal(err)
	}
	saved, err := config.Load(cfgPath)
	if err != nil || !saved.Policy.AllowWrite || saved.Profiles["extra"].Kind != "gitlab" || len(saved.Profiles["gh"].Repos) != 1 {
		t.Fatalf("saved = %+v, err = %v", saved, err)
	}
	if len(cfg.Profiles["gh"].Repos) != 1 {
		t.Fatal("the in-memory profile was not updated")
	}
}

// The bind result comes from the saved file: a profile that was removed from the file meanwhile cannot block it.
func TestBindUsesTheFileNotTheStaleCopy(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	cfg.Policy.AllowWrite = true
	cfg.Profiles = map[string]config.Profile{
		"gh":    {Kind: "github", URL: "https://api.github.com", VerifyTLS: true},
		"other": {Kind: "github", URL: "https://api.github.com", VerifyTLS: true, Repos: []string{"o/*"}},
	}
	cfg.SetPath(cfgPath)
	onDisk := config.Default()
	onDisk.Policy.AllowWrite = true
	onDisk.Profiles = map[string]config.Profile{"gh": cfg.Profiles["gh"]}
	onDisk.SetPath(cfgPath)
	if err := onDisk.Save(); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"bind_profile": "gh", "bind_repo": "o/*"}
	if _, err := CallTool(context.Background(), cfg, "accounts", args); err != nil {
		t.Fatalf("bind blocked by a profile that is no longer in the file: %v", err)
	}
	if saved, _ := config.Load(cfgPath); len(saved.Profiles["gh"].Repos) != 1 {
		t.Fatal("the bind was not saved")
	}
	if len(cfg.Profiles["gh"].Repos) != 1 {
		t.Fatal("the in-memory profile was not updated")
	}
}

func TestSetTokenKeepsTheOldTokenWhenTheNewOneIsAnotherAccounts(t *testing.T) {
	keyring.MockInit()
	t.Setenv("TEST_BIND_OVERRIDE", "personal-token")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer personal-token" {
			_, _ = io.WriteString(w, `{"login":"przestaw"}`)
			return
		}
		_, _ = io.WriteString(w, `{"login":"cads-bot"}`)
	}))
	t.Cleanup(srv.Close)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{"gh": {Kind: "github", URL: srv.URL, VerifyTLS: true, Username: "przestaw", TokenEnv: "TEST_BIND_OVERRIDE"}}
	cfg.SetPath(cfgPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	account := secret.Account("github", "gh")
	if err := config.TokenStore.Set(account, "old-token"); err != nil {
		t.Fatal(err)
	}
	old := cliutil.Stdin
	cliutil.Stdin = strings.NewReader("work-token\n")
	defer func() { cliutil.Stdin = old }()
	if err := Command(cfgPath, "", []string{"set-token", "--profile", "gh", "--stdin"}); err == nil || !strings.Contains(err.Error(), "another account") {
		t.Fatalf("set-token with another account's token: %v", err)
	}
	if val, _ := config.TokenStore.Get(account); val != "old-token" {
		t.Fatalf("token in keyring = %q, want the old one", val)
	}
}

func TestAccountsFiltersTheListByKindAndResolvesIndependently(t *testing.T) {
	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{
		"gh":   {Kind: "github", URL: "https://api.github.com", TokenEnv: "T", Repos: []string{"owner/*"}},
		"gl":   {Kind: "gitlab", URL: "https://gitlab.example/api/v4", TokenEnv: "T", Repos: []string{"group/*"}},
		"jira": {Kind: "jira", URL: "https://jira.example", TokenEnv: "T", Projects: []string{"PROJ"}},
		"misc": {Kind: "api", URL: "https://svc.example", TokenEnv: "T"},
	}
	cfg.Defaults = map[string]string{"github": "gh", "gitlab": "gl", "jira": "jira", "api": "misc"}
	cases := []struct {
		name, kind, resource string
		kinds                []string
		resolved             string
		wantError            bool
	}{
		{"kind only", "github", "", []string{"github"}, "gh", false},
		{"resource only lists everything", "", "owner/repo", []string{"api", "github", "gitlab", "jira"}, "gh", false},
		{"kind and resource", "github", "owner/repo", []string{"github"}, "gh", false},
		{"unknown kind", "nonsense", "", nil, "", true},
		{"resource bound to another kind falls back to the kind's default", "gitlab", "owner/repo", []string{"gitlab"}, "gl", false},
		{"kind without profiles", "azure", "owner/repo", nil, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := CallTool(context.Background(), cfg, "accounts", map[string]any{"kind": c.kind, "resource": c.resource})
			if err != nil {
				t.Fatal(err)
			}
			var rep AccountsReport
			if err := json.Unmarshal(out, &rep); err != nil {
				t.Fatal(err)
			}
			var kinds []string
			for _, p := range rep.Profiles {
				kinds = append(kinds, p.Kind)
			}
			sort.Strings(kinds)
			if strings.Join(kinds, ",") != strings.Join(c.kinds, ",") {
				t.Errorf("profile kinds %v, want %v", kinds, c.kinds)
			}
			if rep.ResolvedProfile != c.resolved {
				t.Errorf("resolved %q, want %q", rep.ResolvedProfile, c.resolved)
			}
			if (rep.ResolutionError != "") != c.wantError {
				t.Errorf("resolution_error %q, want error=%v", rep.ResolutionError, c.wantError)
			}
		})
	}
}

// Google profiles are kind api with a service preset; --kind google must still list them.
func TestAccountsKindGoogleListsTheGoogleProfiles(t *testing.T) {
	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{
		"gdrive": {Kind: "api", Service: "drive", URL: "https://www.googleapis.com/drive/v3", Auth: "oauth2"},
		"misc":   {Kind: "api", URL: "https://svc.example", TokenEnv: "T"},
		"gh":     {Kind: "github", URL: "https://api.github.com", TokenEnv: "T"},
	}
	out, err := CallTool(context.Background(), cfg, "accounts", map[string]any{"kind": "google"})
	if err != nil {
		t.Fatal(err)
	}
	var rep AccountsReport
	if err := json.Unmarshal(out, &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Profiles) != 1 || rep.Profiles[0].Name != "gdrive" {
		t.Fatalf("kind google lists %+v, want only gdrive", rep.Profiles)
	}
	if rep.ResolutionError != "" || rep.ResolvedProfile != "" {
		t.Fatalf("kind google must not try to resolve: %+v", rep)
	}
}

// saveConfig writes a config with the given profiles to a temp file and returns its path.
func saveConfig(t *testing.T, profiles map[string]config.Profile) string {
	t.Helper()
	cfg := config.Default()
	cfg.Profiles = profiles
	cfg.SetPath(filepath.Join(t.TempDir(), "config.json"))
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	return cfg.Path()
}

func TestAuthStatusJSONHasStatesAndFixesButNoSecret(t *testing.T) {
	keyring.MockInit()
	t.Setenv("TEST_STATUS_ENV", "env-secret-value")
	path := saveConfig(t, map[string]config.Profile{
		"stored":  {Kind: "github", URL: "https://api.github.com", VerifyTLS: true},
		"empty":   {Kind: "gitlab", URL: "https://gitlab.example", VerifyTLS: true},
		"fromenv": {Kind: "jira", URL: "https://jira.example", TokenEnv: "TEST_STATUS_ENV", VerifyTLS: true},
		"oauth":   {Kind: "api", URL: "https://api.example", Auth: "oauth2", VerifyTLS: true, OAuth2: &config.OAuth2{ClientID: "id", TokenURL: "https://api.example/token"}},
		"open":    {Kind: "api", URL: "https://open.example", Auth: "none", VerifyTLS: true},
	})
	if err := config.TokenStore.Set(secret.Account("github", "stored"), "keyring-secret-value"); err != nil {
		t.Fatal(err)
	}
	if err := config.TokenStore.Set(secret.Account("api", "oauth"), `{"client_secret":"oauth-secret-value"}`); err != nil {
		t.Fatal(err)
	}

	var runErr error
	out := testutil.CaptureStdout(t, func() { runErr = Command(path, "", []string{"status", "--json"}) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	for _, secretValue := range []string{"env-secret-value", "keyring-secret-value", "oauth-secret-value"} {
		if strings.Contains(out, secretValue) {
			t.Fatalf("status leaked %q: %s", secretValue, out)
		}
	}
	var entries []statusEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("not one JSON array: %v\n%s", err, out)
	}
	type state struct{ state, source, fix string }
	got := map[string]state{}
	for _, e := range entries {
		got[e.Name] = state{e.State, e.Source, e.Fix}
	}
	want := map[string]state{
		"stored":  {StateOK, "keyring", ""},
		"empty":   {StateMissing, "", "workctl auth set-token --profile empty"},
		"fromenv": {StateOK, "env", ""},
		"oauth":   {StateLoginNeed, "keyring", "workctl auth login --profile oauth"},
		"open":    {StateNotNeeded, "", ""},
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: %+v, want %+v", name, got[name], w)
		}
	}
}

func TestAuthSetTokenJSONIsOneDocumentWithoutTheToken(t *testing.T) {
	keyring.MockInit()
	path := saveConfig(t, map[string]config.Profile{"gh": {Kind: "github", URL: "https://127.0.0.1:1", VerifyTLS: true}})
	old := cliutil.Stdin
	cliutil.Stdin = strings.NewReader("tok-should-not-print\n")
	defer func() { cliutil.Stdin = old }()

	var runErr error
	out := testutil.CaptureStdout(t, func() {
		runErr = Command(path, "", []string{"set-token", "--profile", "gh", "--stdin", "--json"})
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	var res struct{ Profile, Action, Warning string }
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.Profile != "gh" || res.Action != "token_stored" || res.Warning == "" {
		t.Fatalf("result %+v, %v\n%s", res, err, out)
	}
	if strings.Contains(out, "tok-should-not-print") {
		t.Fatalf("the token is in the output: %s", out)
	}
}

func TestAuthLoginExistingBindsWithoutStartingALogin(t *testing.T) {
	path := saveConfig(t, map[string]config.Profile{"gh": {Kind: "github", URL: "https://api.github.com", Backend: config.BackendCLI, VerifyTLS: true}})

	calls := testutil.FakeCLI(t, "auth") // gh is not logged in
	err := Command(path, "", []string{"login", "--profile", "gh", "--existing"})
	if err == nil || !strings.Contains(err.Error(), "auth login --profile gh") {
		t.Fatalf("login --existing without a login: %v", err)
	}
	for _, call := range *calls {
		if strings.Contains(strings.Join(call, " "), "auth login") {
			t.Fatalf("--existing started a login: %v", call)
		}
	}

	testutil.FakeCLI(t, "user")
	var runErr error
	out := testutil.CaptureStdout(t, func() {
		runErr = Command(path, "", []string{"login", "--profile", "gh", "--existing", "--json"})
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	var res struct{ Profile, Action, Username string }
	if err := json.Unmarshal([]byte(out), &res); err != nil || res != (struct{ Profile, Action, Username string }{"gh", "bound", "przestaw"}) {
		t.Fatalf("result %+v, %v\n%s", res, err, out)
	}
	if saved, err := config.Load(path); err != nil || saved.Profiles["gh"].Username != "przestaw" {
		t.Fatalf("profile not bound: %+v, %v", saved.Profiles["gh"], err)
	}
}
