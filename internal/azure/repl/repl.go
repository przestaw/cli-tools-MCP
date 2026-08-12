// Package repl implements the interactive `azure shell`.
package repl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/c-bata/go-prompt"
	"workctl/internal/azure/client"
	"workctl/internal/azure/output"
	"workctl/internal/azure/service"
)

// Session is one interactive shell session.
type Session struct {
	svc *service.Service
	p   *output.Printer
	ctx navCtx

	// caches for completion (filled lazily by ls/listing commands)
	projects  []client.Project
	repos     []client.Repository
	prs       []client.PullRequest
	pipelines []client.Pipeline
}

// New builds a Session, starting in the profile's default project if set.
func New(svc *service.Service, p *output.Printer) *Session {
	s := &Session{svc: svc, p: p}
	if svc.DefaultProject != "" {
		s.ctx.level = levelProject
		s.ctx.project = svc.DefaultProject
	}
	return s
}

// Run starts the interactive prompt loop (blocks until exit).
func (s *Session) Run() {
	fmt.Println("azure shell - type `help` for commands, `exit` to quit")
	pr := prompt.New(
		s.execute,
		s.complete,
		prompt.OptionLivePrefix(func() (string, bool) {
			return "azure " + s.ctx.path() + " > ", true
		}),
		prompt.OptionTitle("azure shell"),
	)
	pr.Run()
}

// c returns a per-command timeout context.
func (s *Session) c() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}

// project returns the project in context (error at root).
func (s *Session) project() (string, error) {
	if s.ctx.project == "" {
		return "", fmt.Errorf("no project in context (cd <project>)")
	}
	return s.ctx.project, nil
}

// execute dispatches one shell input line.
func (s *Session) execute(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	fields := strings.Fields(line)
	cmd, args := fields[0], fields[1:]
	rest := strings.TrimSpace(strings.TrimPrefix(line, cmd))

	var err error
	switch cmd {
	case "exit", "quit":
		stop()
	case "help":
		printHelp()
	case "pwd":
		fmt.Println(s.ctx.path())
	case "ls":
		err = s.ls()
	case "cd":
		err = s.cd(args)
	case "use":
		err = s.use(args)
	case "repos":
		err = s.listRepos()
	case "prs":
		err = s.listPRs(args)
	case "pr":
		err = s.showPR(args)
	case "vote":
		err = s.vote(args)
	case "comment":
		err = s.comment(args, rest)
	case "pipelines":
		err = s.listPipelines()
	case "runs":
		err = s.listRuns(args)
	case "builds":
		err = s.listBuilds()
	case "build":
		err = s.showBuild(args)
	case "releases":
		err = s.listReleases()
	case "release":
		err = s.showRelease(args)
	case "branches", "refs":
		err = s.listBranches(args)
	case "files":
		err = s.listFiles(args)
	case "get":
		err = s.get(args)
	case "open":
		err = s.open(args)
	case "expand":
		s.p.Expand = !s.p.Expand
		fmt.Printf("expand: %v\n", s.p.Expand)
	default:
		fmt.Printf("unknown command %q (try `help`)\n", cmd)
	}
	if err != nil {
		fmt.Println("error:", err)
	}
}

// --- navigation ---

func (s *Session) ls() error {
	switch s.ctx.level {
	case levelRoot:
		return s.listProjects()
	case levelProject:
		return s.listRepos()
	case levelRepo:
		return s.listBranches(nil)
	}
	return nil
}

func (s *Session) cd(args []string) error {
	if len(args) == 0 {
		s.ctx.reset()
		return nil
	}
	switch args[0] {
	case "/":
		s.ctx.reset()
		return nil
	case "..":
		s.ctx.up()
		return nil
	}
	switch s.ctx.level {
	case levelRoot:
		s.ctx.project = args[0]
		s.ctx.level = levelProject
		return nil
	case levelProject:
		s.ctx.repo = args[0]
		s.ctx.level = levelRepo
		return nil
	default:
		return fmt.Errorf("cannot cd from here (use `cd ..`)")
	}
}

func (s *Session) use(args []string) error {
	if len(args) != 2 || args[0] != "project" {
		return fmt.Errorf("usage: use project <name>")
	}
	s.ctx.reset()
	s.ctx.level = levelProject
	s.ctx.project = args[1]
	return nil
}

// --- listings ---

func (s *Session) listProjects() error {
	c, cancel := s.c()
	defer cancel()
	ps, err := s.svc.Client.Projects(c, true)
	if err != nil {
		return err
	}
	s.projects = ps
	rows := make([][]string, len(ps))
	for i, p := range ps {
		rows[i] = []string{p.Name, p.State, p.Visibility}
	}
	return s.p.Table([]string{"Name", "State", "Visibility"}, rows)
}

func (s *Session) listRepos() error {
	project, err := s.project()
	if err != nil {
		return err
	}
	c, cancel := s.c()
	defer cancel()
	repos, err := s.svc.Client.Repositories(c, project)
	if err != nil {
		return err
	}
	s.repos = repos
	rows := make([][]string, len(repos))
	for i, r := range repos {
		rows[i] = []string{r.Name, service.ShortRef(r.DefaultBranch)}
	}
	return s.p.Table([]string{"Name", "Default branch"}, rows)
}

func (s *Session) listPRs(args []string) error {
	project, err := s.project()
	if err != nil {
		return err
	}
	status := "active"
	if len(args) > 0 {
		status = args[0]
	}
	c, cancel := s.c()
	defer cancel()
	prs, err := s.svc.Client.PullRequests(c, project, s.ctx.repo, client.PullRequestSearch{Status: status, Top: 50}, false)
	if err != nil {
		return err
	}
	s.prs = prs
	rows := make([][]string, len(prs))
	for i, pr := range prs {
		rows[i] = []string{
			fmt.Sprintf("%d", pr.PullRequestID), pr.Title, pr.Repository.Name,
			service.ShortRef(pr.SourceRefName) + " -> " + service.ShortRef(pr.TargetRefName),
			output.UserName(pr.CreatedBy),
		}
	}
	return s.p.Table([]string{"ID", "Title", "Repo", "Branches", "Author"}, rows)
}

func (s *Session) showPR(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: pr <id>")
	}
	project, err := s.project()
	if err != nil {
		return err
	}
	id, err := atoi(args[0])
	if err != nil {
		return err
	}
	c, cancel := s.c()
	defer cancel()
	pr, err := s.svc.Client.PullRequest(c, project, id)
	if err != nil {
		return err
	}
	return s.p.KeyValue([][2]string{
		{"ID", fmt.Sprintf("%d", pr.PullRequestID)},
		{"Title", pr.Title},
		{"Status", pr.Status},
		{"Repo", pr.Repository.Name},
		{"Source", service.ShortRef(pr.SourceRefName)},
		{"Target", service.ShortRef(pr.TargetRefName)},
		{"Author", output.UserName(pr.CreatedBy)},
		{"Description", pr.Description},
	})
}

func (s *Session) vote(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: vote <id> <approve|approve-suggestions|reset|wait|reject>")
	}
	project, err := s.project()
	if err != nil {
		return err
	}
	id, err := atoi(args[0])
	if err != nil {
		return err
	}
	vote, err := service.ParseVote(args[1])
	if err != nil {
		return err
	}
	c, cancel := s.c()
	defer cancel()
	pr, err := s.svc.Client.PullRequest(c, project, id)
	if err != nil {
		return err
	}
	me, err := s.svc.CurrentUserID(c)
	if err != nil {
		return err
	}
	if err := s.svc.Client.SetReviewerVote(c, project, pr.Repository.ID, id, me, vote); err != nil {
		return err
	}
	fmt.Printf("voted %q on PR !%d\n", service.VoteName(vote), id)
	return nil
}

func (s *Session) comment(args []string, rest string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: comment <id> <text>")
	}
	project, err := s.project()
	if err != nil {
		return err
	}
	id, err := atoi(args[0])
	if err != nil {
		return err
	}
	text := strings.TrimSpace(strings.TrimPrefix(rest, args[0]))
	c, cancel := s.c()
	defer cancel()
	pr, err := s.svc.Client.PullRequest(c, project, id)
	if err != nil {
		return err
	}
	th, err := s.svc.Client.AddPRComment(c, project, pr.Repository.ID, id, text, nil)
	if err != nil {
		return err
	}
	fmt.Printf("added comment (thread %d)\n", th.ID)
	return nil
}

func (s *Session) listPipelines() error {
	project, err := s.project()
	if err != nil {
		return err
	}
	c, cancel := s.c()
	defer cancel()
	pls, err := s.svc.Client.Pipelines(c, project, true)
	if err != nil {
		return err
	}
	s.pipelines = pls
	rows := make([][]string, len(pls))
	for i, pl := range pls {
		rows[i] = []string{fmt.Sprintf("%d", pl.ID), pl.Name, pl.Folder}
	}
	return s.p.Table([]string{"ID", "Name", "Folder"}, rows)
}

func (s *Session) listRuns(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: runs <pipelineId>")
	}
	project, err := s.project()
	if err != nil {
		return err
	}
	id, err := atoi(args[0])
	if err != nil {
		return err
	}
	c, cancel := s.c()
	defer cancel()
	runs, err := s.svc.Client.PipelineRuns(c, project, id)
	if err != nil {
		return err
	}
	rows := make([][]string, len(runs))
	for i, r := range runs {
		rows[i] = []string{fmt.Sprintf("%d", r.ID), r.Name, r.State, r.Result}
	}
	return s.p.Table([]string{"Run", "Name", "State", "Result"}, rows)
}

func (s *Session) listBuilds() error {
	project, err := s.project()
	if err != nil {
		return err
	}
	c, cancel := s.c()
	defer cancel()
	builds, err := s.svc.Client.Builds(c, project, client.BuildSearch{Top: 30}, false)
	if err != nil {
		return err
	}
	rows := make([][]string, len(builds))
	for i, b := range builds {
		rows[i] = []string{fmt.Sprintf("%d", b.ID), b.BuildNumber, b.Definition.Name, b.Status, b.Result}
	}
	return s.p.Table([]string{"ID", "Number", "Definition", "Status", "Result"}, rows)
}

func (s *Session) showBuild(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: build <id>")
	}
	project, err := s.project()
	if err != nil {
		return err
	}
	id, err := atoi(args[0])
	if err != nil {
		return err
	}
	c, cancel := s.c()
	defer cancel()
	b, err := s.svc.Client.Build(c, project, id)
	if err != nil {
		return err
	}
	return s.p.KeyValue([][2]string{
		{"ID", fmt.Sprintf("%d", b.ID)},
		{"Number", b.BuildNumber},
		{"Definition", b.Definition.Name},
		{"Status", b.Status},
		{"Result", b.Result},
		{"Branch", service.ShortRef(b.SourceBranch)},
	})
}

func (s *Session) listReleases() error {
	project, err := s.project()
	if err != nil {
		return err
	}
	c, cancel := s.c()
	defer cancel()
	rels, err := s.svc.Client.Releases(c, project, client.ReleaseSearch{Top: 30}, false)
	if err != nil {
		return err
	}
	rows := make([][]string, len(rels))
	for i, r := range rels {
		rows[i] = []string{fmt.Sprintf("%d", r.ID), r.Name, r.ReleaseDefinition.Name, r.Status}
	}
	return s.p.Table([]string{"ID", "Name", "Definition", "Status"}, rows)
}

func (s *Session) showRelease(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: release <id>")
	}
	project, err := s.project()
	if err != nil {
		return err
	}
	id, err := atoi(args[0])
	if err != nil {
		return err
	}
	c, cancel := s.c()
	defer cancel()
	r, err := s.svc.Client.Release(c, project, id)
	if err != nil {
		return err
	}
	var stages []string
	for _, e := range r.Environments {
		stages = append(stages, fmt.Sprintf("%s:%s", e.Name, e.Status))
	}
	return s.p.KeyValue([][2]string{
		{"ID", fmt.Sprintf("%d", r.ID)},
		{"Name", r.Name},
		{"Definition", r.ReleaseDefinition.Name},
		{"Status", r.Status},
		{"Stages", strings.Join(stages, ", ")},
	})
}

func (s *Session) listBranches(args []string) error {
	project, err := s.project()
	if err != nil {
		return err
	}
	repo := s.ctx.repo
	if len(args) > 0 {
		repo = args[0]
	}
	if repo == "" {
		return fmt.Errorf("usage: branches <repo> (or cd into a repo)")
	}
	c, cancel := s.c()
	defer cancel()
	refs, err := s.svc.Client.Refs(c, project, repo, "heads")
	if err != nil {
		return err
	}
	rows := make([][]string, len(refs))
	for i, r := range refs {
		rows[i] = []string{service.ShortRef(r.Name), r.ObjectID}
	}
	return s.p.Table([]string{"Branch", "Commit"}, rows)
}

func (s *Session) listFiles(args []string) error {
	project, err := s.project()
	if err != nil {
		return err
	}
	repo := s.ctx.repo
	path := "/"
	switch {
	case s.ctx.level == levelRepo && len(args) >= 1:
		path = args[0]
	case len(args) >= 2:
		repo, path = args[0], args[1]
	case len(args) == 1 && repo == "":
		repo = args[0]
	}
	if repo == "" {
		return fmt.Errorf("usage: files <repo> [path] (or cd into a repo)")
	}
	c, cancel := s.c()
	defer cancel()
	items, err := s.svc.Client.Items(c, project, repo, path, "", "oneLevel")
	if err != nil {
		return err
	}
	rows := make([][]string, len(items))
	for i, it := range items {
		kind := "file"
		if it.IsFolder {
			kind = "dir"
		}
		rows[i] = []string{kind, it.Path}
	}
	return s.p.Table([]string{"Type", "Path"}, rows)
}

func (s *Session) get(args []string) error {
	switch s.ctx.level {
	case levelRepo:
		project, _ := s.project()
		c, cancel := s.c()
		defer cancel()
		r, err := s.svc.Client.Repository(c, project, s.ctx.repo)
		if err != nil {
			return err
		}
		return s.p.KeyValue([][2]string{
			{"Name", r.Name},
			{"Default branch", service.ShortRef(r.DefaultBranch)},
			{"Clone (HTTPS)", r.RemoteURL},
		})
	default:
		if len(args) > 0 {
			return s.showPR(args)
		}
		return fmt.Errorf("get needs an id here, or cd into a repo")
	}
}

func (s *Session) open(args []string) error {
	project, err := s.project()
	if err != nil {
		return err
	}
	switch {
	case len(args) > 0:
		id, err := atoi(args[0])
		if err != nil {
			return err
		}
		c, cancel := s.c()
		defer cancel()
		pr, err := s.svc.Client.PullRequest(c, project, id)
		if err != nil {
			return err
		}
		return openBrowser(s.svc.Client.PRWebURL(project, pr.Repository.Name, id))
	case s.ctx.level == levelRepo:
		return openBrowser(s.svc.Client.RepoWebURL(project, s.ctx.repo))
	default:
		return openBrowser(s.svc.OrgURL + "/" + project)
	}
}

// atoi parses an integer argument.
func atoi(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("expected a number, got %q", s)
		}
		n = n*10 + int(r-'0')
	}
	if s == "" {
		return 0, fmt.Errorf("expected a number")
	}
	return n, nil
}
