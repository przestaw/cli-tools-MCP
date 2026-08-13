// Package repl implements the interactive `atlassian shell`.
package repl

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/c-bata/go-prompt"
	"workctl/internal/atlassian/client"
	"workctl/internal/atlassian/output"
	"workctl/internal/atlassian/service"
	"workctl/internal/atlassian/wizard"
)

// Session is one interactive shell session.
type Session struct {
	svc *service.Service
	p   *output.Printer
	ctx navCtx

	// caches for completion + ls (filled lazily)
	projects []client.Project
	issues   []client.Issue
	spaces   []client.Space
	entries  []client.ContentChild // wiki nodes listed by the last ls

	more lsState // continuation of the last listing (`ls more` / `ls all`)
}

// lsState remembers how to continue the previous listing.
type lsState struct {
	kind    string // "" | "jql" | "space" | "children"
	jql     string // jql kind: the query
	token   string // jql kind: next page token
	cursor  string // wiki kinds: next cursor
	nodeID  string // children kind: parent node
	folder  bool   // children kind: parent is a folder
	spaceID string // space kind: the space being listed
	sort    string // space kind: v2 sort parameter of the listing
}

// hasMore reports whether a continuation exists.
func (st lsState) hasMore() bool {
	return st.token != "" || st.cursor != ""
}

// lsArgs are the parsed optional arguments of the ls command.
type lsArgs struct {
	mode  string // "" | "more" | "all"
	field string // sort field ("" = context default)
	dir   string // "" | "asc" | "desc"
}

// parseLsArgs parses `ls [more|all] [by <field>] [asc|desc]` in any order.
func parseLsArgs(args []string) (lsArgs, error) {
	var out lsArgs
	usage := "usage: ls [more|all] [by <field>] [asc|desc]"
	for i := 0; i < len(args); i++ {
		switch a := strings.ToLower(args[i]); a {
		case "more", "all":
			out.mode = a
		case "by":
			if i+1 >= len(args) {
				return out, fmt.Errorf("%s", usage)
			}
			i++
			out.field = strings.ToLower(args[i])
		case "asc", "desc":
			out.dir = a
		default:
			return out, fmt.Errorf("unknown ls argument %q (%s)", args[i], usage)
		}
	}
	if out.mode == "more" && (out.field != "" || out.dir != "") {
		return out, fmt.Errorf("`ls more` continues the current listing; re-sort with `ls by <field>` first")
	}
	return out, nil
}

// jqlSortFields maps ls sort fields to JQL ORDER BY fields for issue listings.
var jqlSortFields = map[string]string{
	"updated": "updated", "created": "created", "key": "key",
	"priority": "priority", "status": "status", "summary": "summary",
	"duedate": "duedate", "rank": "Rank",
}

// spaceSortFields maps ls sort fields to Confluence v2 page sort params.
var spaceSortFields = map[string]string{
	"title": "title", "modified": "modified-date", "created": "created-date", "id": "id",
}

// sortKeys lists a sort map's keys for error messages.
func sortKeys(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// lsPageSize is the page size of shell listings.
const lsPageSize = 50

// lsAllCap bounds `ls all` against unbounded result sets.
const lsAllCap = 1000

// New builds a Session.
func New(svc *service.Service, p *output.Printer) *Session {
	return &Session{svc: svc, p: p}
}

// Run starts the interactive prompt loop (blocks until exit).
func (s *Session) Run() {
	fmt.Println("atlassian shell - type `help` for commands, `exit` to quit")
	pr := prompt.New(
		s.execute,
		s.complete,
		prompt.OptionLivePrefix(func() (string, bool) {
			return "atlassian " + s.ctx.path() + " > ", true
		}),
		prompt.OptionTitle("atlassian shell"),
	)
	pr.Run()
}

// c returns a per-command timeout context.
func (s *Session) c() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
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
		var la lsArgs
		if la, err = parseLsArgs(args); err != nil {
			break
		}
		switch {
		case la.mode == "more":
			err = s.lsMore(false)
		case la.mode == "all":
			if s.more.kind == "" || la.field != "" || la.dir != "" {
				// fresh (possibly re-sorted) listing, then drain it
				if err = s.ls(la.field, la.dir); err == nil && s.more.hasMore() {
					err = s.lsMore(true)
				}
			} else {
				err = s.lsMore(true)
			}
		default:
			err = s.ls(la.field, la.dir)
		}
	case "cd":
		err = s.cd(args)
	case "use":
		err = s.use(args)
	case "get":
		err = s.get(args)
	case "jql", "query":
		err = s.jql(rest)
	case "cql":
		err = s.cql(rest)
	case "find":
		err = s.find(rest)
	case "mywork":
		err = s.jql("assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC")
	case "comment":
		err = s.comment(args, rest)
	case "transition":
		err = s.transition(args)
	case "set-field":
		err = s.setField(args)
	case "create":
		err = s.create(rest)
	case "sprint":
		err = s.sprint(args)
	case "open":
		err = s.open(args)
	case "trace":
		err = s.trace(args)
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

func (s *Session) ls(field, dir string) error {
	c, cancel := s.c()
	defer cancel()
	switch s.ctx.level {
	case levelRoot:
		return s.p.Table([]string{"Scope", "Description"}, [][]string{
			{"jira", "Jira projects (cd jira)"},
			{"wiki", "Confluence spaces (cd wiki)"},
		})
	case levelJira:
		ps, err := s.svc.Client.ProjectsAll(c)
		if err != nil {
			return err
		}
		if err := sortByField(ps, field, dir, map[string]func(client.Project) string{
			"key":  func(p client.Project) string { return p.Key },
			"name": func(p client.Project) string { return p.Name },
		}); err != nil {
			return err
		}
		s.projects = ps
		rows := make([][]string, 0, len(ps))
		for _, pr := range ps {
			rows = append(rows, []string{pr.Key, pr.Name, pr.TypeKey})
		}
		return s.p.Table([]string{"Key", "Name", "Type"}, rows)
	case levelProject:
		order, err := jqlOrderClause(field, dir)
		if err != nil {
			return err
		}
		return s.listIssues("project = " + s.ctx.projectKey + order)
	case levelIssue:
		is, err := s.svc.Client.Issue(c, s.ctx.issueKey, "")
		if err != nil {
			return err
		}
		subtasks := append([]client.IssueRef(nil), is.Fields.Subtasks...)
		if err := sortByField(subtasks, field, dir, map[string]func(client.IssueRef) string{
			"key": func(r client.IssueRef) string { return r.Key },
			"summary": func(r client.IssueRef) string {
				if r.Fields != nil {
					return r.Fields.Summary
				}
				return ""
			},
			"status": func(r client.IssueRef) string {
				if r.Fields != nil && r.Fields.Status != nil {
					return r.Fields.Status.Name
				}
				return ""
			},
		}); err != nil {
			return err
		}
		rows := make([][]string, len(subtasks))
		for i, st := range subtasks {
			summary, status := "", ""
			if st.Fields != nil {
				summary = st.Fields.Summary
				if st.Fields.Status != nil {
					status = st.Fields.Status.Name
				}
			}
			rows[i] = []string{st.Key, summary, status}
		}
		return s.p.Table([]string{"Key", "Summary", "Status"}, rows)
	case levelWiki:
		sps, err := s.svc.Client.SpacesAll(c, nil)
		if err != nil {
			return err
		}
		if err := sortByField(sps, field, dir, map[string]func(client.Space) string{
			"key":  func(sp client.Space) string { return sp.Key },
			"name": func(sp client.Space) string { return sp.Name },
		}); err != nil {
			return err
		}
		s.spaces = sps
		rows := make([][]string, len(sps))
		for i, sp := range sps {
			rows[i] = []string{sp.Key, sp.Name, sp.Type}
		}
		return s.p.Table([]string{"Key", "Name", "Type"}, rows)
	case levelSpace:
		// Filesystem-style: root folders first, then root pages.
		sortParam, err := spaceSortParam(field, dir)
		if err != nil {
			return err
		}
		folders, err := s.svc.Client.SpaceRootFolders(c, s.ctx.spaceKey, 100)
		if err != nil {
			return err
		}
		if dir == "desc" {
			reverseSlice(folders)
		}
		pages, cursor, err := s.svc.Client.SpaceRootPages(c, s.ctx.spaceID, "", lsPageSize, sortParam)
		if err != nil {
			return err
		}
		entries := folders
		for _, pg := range pages {
			entries = append(entries, client.ContentChild{ID: pg.ID, Title: pg.Title, Type: "page", Status: pg.Status})
		}
		s.more = lsState{kind: "space", cursor: cursor, spaceID: s.ctx.spaceID, sort: sortParam}
		return s.listEntries(entries, false)
	case levelPage:
		var (
			children []client.ContentChild
			cursor   string
			err      error
		)
		if s.ctx.isFolder {
			children, cursor, err = s.svc.Client.FolderDirectChildren(c, s.ctx.pageID, "", lsPageSize)
		} else {
			children, cursor, err = s.svc.Client.PageDirectChildren(c, s.ctx.pageID, "", lsPageSize)
		}
		if err != nil {
			return err
		}
		if err := sortChildren(children, field, dir); err != nil {
			return err
		}
		s.more = lsState{kind: "children", nodeID: s.ctx.pageID, folder: s.ctx.isFolder, cursor: cursor}
		return s.listEntries(sortFoldersFirst(children), false)
	}
	return nil
}

// jqlOrderClause builds the ORDER BY clause for project issue listings.
func jqlOrderClause(field, dir string) (string, error) {
	if field == "" {
		field = "updated"
	}
	jqlField, ok := jqlSortFields[field]
	if !ok {
		return "", fmt.Errorf("cannot sort issues by %q (available: %s)", field, sortKeys(jqlSortFields))
	}
	if dir == "" {
		dir = "asc"
		if field == "updated" || field == "created" {
			dir = "desc" // newest first feels right for dates
		}
	}
	return " ORDER BY " + jqlField + " " + strings.ToUpper(dir), nil
}

// spaceSortParam maps ls sort args to the v2 pages sort parameter.
func spaceSortParam(field, dir string) (string, error) {
	if field == "" {
		field = "title"
	}
	param, ok := spaceSortFields[field]
	if !ok {
		return "", fmt.Errorf("cannot sort pages by %q (available: %s)", field, sortKeys(spaceSortFields))
	}
	if dir == "" && (field == "modified" || field == "created") {
		dir = "desc"
	}
	if dir == "desc" {
		param = "-" + param
	}
	return param, nil
}

// sortChildren sorts direct children client-side (only title is available).
func sortChildren(children []client.ContentChild, field, dir string) error {
	switch field {
	case "":
		return nil // keep the API's tree (position) order
	case "title":
		sort.SliceStable(children, func(i, j int) bool {
			if dir == "desc" {
				return children[i].Title > children[j].Title
			}
			return children[i].Title < children[j].Title
		})
		return nil
	default:
		return fmt.Errorf("cannot sort children by %q (available: title)", field)
	}
}

// sortByField sorts a slice client-side by a named string key.
func sortByField[T any](items []T, field, dir string, keys map[string]func(T) string) error {
	if field == "" {
		if dir == "desc" {
			reverseSlice(items)
		}
		return nil
	}
	key, ok := keys[field]
	if !ok {
		return fmt.Errorf("cannot sort by %q (available: %s)", field, funcKeys(keys))
	}
	sort.SliceStable(items, func(i, j int) bool {
		if dir == "desc" {
			return key(items[i]) > key(items[j])
		}
		return key(items[i]) < key(items[j])
	})
	return nil
}

// funcKeys lists a key-func map's names for error messages.
func funcKeys[T any](m map[string]func(T) string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// reverseSlice reverses a slice in place.
func reverseSlice[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// lsMore continues the previous listing: one more page, or everything (all).
func (s *Session) lsMore(all bool) error {
	c, cancel := s.c()
	defer cancel()
	switch s.more.kind {
	case "jql":
		if s.more.token == "" {
			fmt.Println("no more results")
			return nil
		}
		var batch []client.Issue
		for {
			page, err := s.svc.Client.SearchJQL(c, s.more.jql, nil, lsPageSize, s.more.token)
			if err != nil {
				return err
			}
			batch = append(batch, page.Issues...)
			s.more.token = page.NextPageToken
			if page.IsLast || page.NextPageToken == "" {
				s.more.token = ""
			}
			if !all || s.more.token == "" || len(s.issues)+len(batch) >= lsAllCap {
				break
			}
		}
		s.issues = append(s.issues, batch...)
		if err := s.p.IssueTable(batch); err != nil {
			return err
		}
		s.issueStatus(len(batch))
		return nil
	case "space", "children":
		if s.more.cursor == "" {
			fmt.Println("no more results")
			return nil
		}
		var batch []client.ContentChild
		for {
			var (
				pages  []client.Page
				kids   []client.ContentChild
				cursor string
				err    error
			)
			if s.more.kind == "space" {
				pages, cursor, err = s.svc.Client.SpaceRootPages(c, s.more.spaceID, s.more.cursor, lsPageSize, s.more.sort)
				for _, pg := range pages {
					kids = append(kids, client.ContentChild{ID: pg.ID, Title: pg.Title, Type: "page", Status: pg.Status})
				}
			} else {
				if s.more.folder {
					kids, cursor, err = s.svc.Client.FolderDirectChildren(c, s.more.nodeID, s.more.cursor, lsPageSize)
				} else {
					kids, cursor, err = s.svc.Client.PageDirectChildren(c, s.more.nodeID, s.more.cursor, lsPageSize)
				}
			}
			if err != nil {
				return err
			}
			batch = append(batch, kids...)
			s.more.cursor = cursor
			if !all || cursor == "" || len(kids) == 0 || len(s.entries)+len(batch) >= lsAllCap {
				break
			}
		}
		return s.listEntries(append(s.entries, batch...), true)
	default:
		fmt.Println("nothing to continue (run `ls` first)")
		return nil
	}
}

// listEntries caches + renders typed wiki nodes. With appended=true the cache
// is replaced by the passed (already merged) slice and only the tail printed.
func (s *Session) listEntries(entries []client.ContentChild, appended bool) error {
	tail := entries
	if appended {
		tail = entries[len(s.entries):]
	}
	s.entries = entries
	rows := make([][]string, len(tail))
	for i, e := range tail {
		rows[i] = []string{e.Type, e.ID, e.Title}
	}
	if err := s.p.Table([]string{"Type", "ID", "Title"}, rows); err != nil {
		return err
	}
	if s.more.cursor != "" {
		fmt.Printf("%d shown - more available: `ls more` or `ls all`\n", len(entries))
	}
	return nil
}

// sortFoldersFirst stably partitions children: folders, then everything else,
// keeping the API's tree order within each group.
func sortFoldersFirst(children []client.ContentChild) []client.ContentChild {
	out := make([]client.ContentChild, 0, len(children))
	for _, c := range children {
		if c.Type == "folder" {
			out = append(out, c)
		}
	}
	for _, c := range children {
		if c.Type != "folder" {
			out = append(out, c)
		}
	}
	return out
}

func (s *Session) listIssues(jql string) error {
	c, cancel := s.c()
	defer cancel()
	page, err := s.svc.Client.SearchJQL(c, jql, nil, lsPageSize, "")
	if err != nil {
		return err
	}
	s.issues = page.Issues
	token := page.NextPageToken
	if page.IsLast {
		token = ""
	}
	s.more = lsState{kind: "jql", jql: jql, token: token}
	if err := s.p.IssueTable(page.Issues); err != nil {
		return err
	}
	s.issueStatus(len(page.Issues))
	return nil
}

// issueStatus prints how many issues were shown and whether more exist.
func (s *Session) issueStatus(shown int) {
	if s.more.kind != "jql" {
		return
	}
	if s.more.token != "" {
		c, cancel := s.c()
		defer cancel()
		if count, err := s.svc.Client.ApproximateCount(c, s.more.jql); err == nil {
			fmt.Printf("%d shown of ~%d - more available: `ls more` or `ls all`\n", len(s.issues), count)
			return
		}
		fmt.Printf("%d shown - more available: `ls more` or `ls all`\n", len(s.issues))
		return
	}
	if shown == 0 {
		fmt.Println("no results")
	}
}

func (s *Session) cd(args []string) error {
	s.more = lsState{} // navigation invalidates the last listing
	if len(args) == 0 {
		s.ctx.reset()
		return nil
	}
	arg := args[0]
	switch arg {
	case "/":
		s.ctx.reset()
		return nil
	case "..":
		s.ctx.up()
		return nil
	}
	c, cancel := s.c()
	defer cancel()
	switch s.ctx.level {
	case levelRoot, levelJira:
		if s.ctx.level == levelRoot {
			if strings.EqualFold(arg, "wiki") {
				s.ctx.level = levelWiki
				return nil
			}
			if strings.EqualFold(arg, "jira") {
				s.ctx.level = levelJira
				return nil
			}
		}
		s.ctx.projectKey = strings.ToUpper(arg)
		s.ctx.level = levelProject
		return nil
	case levelProject, levelIssue:
		s.ctx.issueKey = s.normalizeIssueKey(arg)
		s.ctx.level = levelIssue
		return nil
	case levelWiki:
		sp, err := s.svc.Client.SpaceByKey(c, arg)
		if err != nil {
			return err
		}
		s.ctx.spaceKey, s.ctx.spaceID = sp.Key, sp.ID
		s.ctx.level = levelSpace
		return nil
	case levelSpace, levelPage:
		isFolder, err := s.resolveWikiNode(c, arg)
		if err != nil {
			return err
		}
		s.ctx.pageID = arg
		s.ctx.isFolder = isFolder
		s.ctx.level = levelPage
		return nil
	}
	return nil
}

// resolveWikiNode determines whether a wiki node id is a folder: first from
// the last ls cache, else by probing the page endpoint then the folder one.
func (s *Session) resolveWikiNode(c context.Context, id string) (bool, error) {
	for _, e := range s.entries {
		if e.ID == id {
			return e.Type == "folder", nil
		}
	}
	if _, err := s.svc.Client.Page(c, id, false); err == nil {
		return false, nil
	} else if !client.IsNotFound(err) {
		return false, err
	}
	if _, err := s.svc.Client.FolderRaw(c, id); err == nil {
		return true, nil
	} else if !client.IsNotFound(err) {
		return false, err
	}
	return false, fmt.Errorf("no page or folder with id %s", id)
}

func (s *Session) use(args []string) error {
	s.more = lsState{}
	if len(args) != 2 {
		return fmt.Errorf("usage: use project <key> | use space <key>")
	}
	switch args[0] {
	case "project":
		s.ctx.reset()
		s.ctx.projectKey = strings.ToUpper(args[1])
		s.ctx.level = levelProject
		return nil
	case "space":
		c, cancel := s.c()
		defer cancel()
		sp, err := s.svc.Client.SpaceByKey(c, args[1])
		if err != nil {
			return err
		}
		s.ctx.reset()
		s.ctx.level = levelSpace
		s.ctx.spaceKey, s.ctx.spaceID = sp.Key, sp.ID
		return nil
	}
	return fmt.Errorf("usage: use project <key> | use space <key>")
}

// --- viewing ---

// inWiki reports whether the context is on the Confluence side.
func (s *Session) inWiki() bool {
	return s.ctx.level == levelWiki || s.ctx.level == levelSpace || s.ctx.level == levelPage
}

func (s *Session) get(args []string) error {
	c, cancel := s.c()
	defer cancel()
	if s.inWiki() {
		id, isFolder, err := s.wikiTarget(c, args)
		if err != nil {
			return err
		}
		if isFolder {
			raw, err := s.svc.Client.FolderRaw(c, id)
			if err != nil {
				return err
			}
			return s.p.Raw(raw)
		}
		pg, err := s.svc.Client.Page(c, id, true)
		if err != nil {
			return err
		}
		return s.p.Page(pg, s.svc.Client.WikiURL(pg.Links.WebUI))
	}
	key, err := s.issueArg(args)
	if err != nil {
		return err
	}
	is, err := s.svc.Client.Issue(c, key, "")
	if err != nil {
		return err
	}
	return s.p.Issue(is, s.svc.BaseURL)
}

// wikiTarget resolves a wiki node id + folderness from args or the context.
func (s *Session) wikiTarget(c context.Context, args []string) (string, bool, error) {
	if len(args) > 0 {
		id := args[0]
		isFolder, err := s.resolveWikiNode(c, id)
		return id, isFolder, err
	}
	if s.ctx.pageID == "" {
		return "", false, fmt.Errorf("no page in context (cd <id> or pass an id)")
	}
	return s.ctx.pageID, s.ctx.isFolder, nil
}

// issueArg resolves the issue key from args or the current context. A bare id
// is expanded against the current project (see normalizeIssueKey).
func (s *Session) issueArg(args []string) (string, error) {
	if len(args) > 0 {
		return s.normalizeIssueKey(args[0]), nil
	}
	if s.ctx.issueKey != "" {
		return s.ctx.issueKey, nil
	}
	return "", fmt.Errorf("no issue in context (cd <key|id> or pass a key)")
}

func (s *Session) jql(q string) error {
	if q == "" {
		return fmt.Errorf("usage: jql <query>")
	}
	return s.listIssues(q)
}

func (s *Session) cql(q string) error {
	if q == "" {
		return fmt.Errorf("usage: cql <query>")
	}
	c, cancel := s.c()
	defer cancel()
	page, err := s.svc.Client.SearchCQL(c, q, 0, 25)
	if err != nil {
		return err
	}
	rows := make([][]string, len(page.Results))
	for i, r := range page.Results {
		space := ""
		if r.Space != nil {
			space = r.Space.Key
		}
		rows[i] = []string{r.ID, r.Type, r.Title, space}
	}
	return s.p.Table([]string{"ID", "Type", "Title", "Space"}, rows)
}

func (s *Session) find(text string) error {
	if text == "" {
		return fmt.Errorf("usage: find <text>")
	}
	if s.inWiki() {
		cql := fmt.Sprintf("type = page AND title ~ %q", text)
		if s.ctx.spaceKey != "" {
			cql = fmt.Sprintf("space = %q AND %s", s.ctx.spaceKey, cql)
		}
		return s.cql(cql)
	}
	return s.listIssues(s.jiraFindJQL(text))
}

// jiraFindJQL builds the JQL for `find` in a jira context. A term that is an
// issue key (AB-1557, or a bare number inside a project) is matched by key:
// JQL `text ~` searches summary/description/comments and never the key, so
// `find AB-1557` would otherwise return nothing even though the issue exists.
// Any other term is a project-scoped full-text search.
func (s *Session) jiraFindJQL(text string) string {
	b := &service.JQLBuilder{}
	if s.isIssueKeyToken(text) {
		return b.Key(s.normalizeIssueKey(text)).String()
	}
	return b.Project(s.ctx.projectKey).Text(text).String()
}

// --- actions ---

func (s *Session) comment(args []string, rest string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: comment list [id] | comment add [id] <text>")
	}
	c, cancel := s.c()
	defer cancel()
	sub, subArgs := args[0], args[1:]
	if s.inWiki() {
		id := s.ctx.pageID
		if len(subArgs) > 0 && isDigits(subArgs[0]) {
			id = subArgs[0]
			subArgs = subArgs[1:]
		}
		if id == "" {
			return fmt.Errorf("no page in context")
		}
		switch sub {
		case "list":
			comments, _, err := s.svc.Client.PageFooterComments(c, id, "", 50)
			if err != nil {
				return err
			}
			rows := make([][]string, len(comments))
			for i, cm := range comments {
				body := ""
				if cm.Body != nil && cm.Body.Storage != nil {
					body = s.p.StorageToText(cm.Body.Storage.Value)
				}
				rows[i] = []string{cm.ID, body}
			}
			return s.p.Table([]string{"ID", "Body"}, rows)
		case "add":
			text := strings.Join(subArgs, " ")
			if text == "" {
				return fmt.Errorf("usage: comment add [id] <text>")
			}
			cm, err := s.svc.Client.AddFooterComment(c, id, client.TextToStorage(text))
			if err != nil {
				return err
			}
			fmt.Printf("added comment %s\n", cm.ID)
			return nil
		}
		return fmt.Errorf("usage: comment list [id] | comment add [id] <text>")
	}

	key := s.ctx.issueKey
	if len(subArgs) > 0 && s.isIssueKeyToken(subArgs[0]) {
		key = s.normalizeIssueKey(subArgs[0])
		subArgs = subArgs[1:]
	}
	if key == "" {
		return fmt.Errorf("no issue in context")
	}
	switch sub {
	case "list":
		page, err := s.svc.Client.Comments(c, key, 0, 50)
		if err != nil {
			return err
		}
		rows := make([][]string, len(page.Comments))
		for i, cm := range page.Comments {
			rows[i] = []string{cm.ID, output.UserName(cm.Author), client.ADFToText(cm.Body)}
		}
		return s.p.Table([]string{"ID", "Author", "Body"}, rows)
	case "add":
		text := strings.Join(subArgs, " ")
		if text == "" {
			return fmt.Errorf("usage: comment add [key] <text>")
		}
		cm, err := s.svc.Client.AddComment(c, key, client.TextToADF(text))
		if err != nil {
			return err
		}
		fmt.Printf("added comment %s\n", cm.ID)
		return nil
	}
	_ = rest
	return fmt.Errorf("usage: comment list [id] | comment add [id] <text>")
}

func (s *Session) transition(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: transition [key] <status>")
	}
	key := s.ctx.issueKey
	if s.isIssueKeyToken(args[0]) && len(args) > 1 {
		key = s.normalizeIssueKey(args[0])
		args = args[1:]
	}
	if key == "" {
		return fmt.Errorf("no issue in context")
	}
	target := strings.Join(args, " ")
	c, cancel := s.c()
	defer cancel()
	id, name, err := s.svc.ResolveTransitionID(c, key, target)
	if err != nil {
		return err
	}
	if err := s.svc.Client.DoTransition(c, key, id); err != nil {
		return err
	}
	fmt.Printf("transitioned %s via %q\n", key, name)
	return nil
}

func (s *Session) setField(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: set-field [key] <field> <value>")
	}
	key := s.ctx.issueKey
	if s.isIssueKeyToken(args[0]) && len(args) >= 3 {
		key = s.normalizeIssueKey(args[0])
		args = args[1:]
	}
	if key == "" {
		return fmt.Errorf("no issue in context")
	}
	field := args[0]
	value := strings.Join(args[1:], " ")
	c, cancel := s.c()
	defer cancel()
	fields, err := s.svc.BuildEditFields(c, key, []service.FieldSpec{{Key: field, Value: value}})
	if err != nil {
		return err
	}
	if err := s.svc.Client.UpdateIssue(c, key, fields); err != nil {
		return err
	}
	fmt.Printf("updated %s\n", key)
	return nil
}

// create makes an issue in the current project. With no arguments it launches
// the interactive form (pick issue type, then prompt every required field);
// `create <summary> [:: <description>]` is the quick path that defaults the
// type to Task.
func (s *Session) create(rest string) error {
	if s.ctx.projectKey == "" || s.inWiki() {
		return fmt.Errorf("create works in a project context (use project <key>)")
	}
	if rest == "" {
		ref, err := wizard.Create(s.svc, s.ctx.projectKey, "", "")
		if err != nil {
			return err
		}
		fmt.Printf("created %s\n", ref.Key)
		return nil
	}
	summary, desc, _ := strings.Cut(rest, "::")
	summary = strings.TrimSpace(summary)
	desc = strings.TrimSpace(desc)

	c, cancel := s.c()
	defer cancel()
	types, err := s.svc.Client.CreateMetaIssueTypes(c, s.ctx.projectKey)
	if err != nil {
		return err
	}
	typeID := ""
	for _, t := range types {
		if !t.Subtask && (strings.EqualFold(t.Name, "Task") || typeID == "") {
			if typeID == "" || strings.EqualFold(t.Name, "Task") {
				typeID = t.ID
			}
		}
	}
	if typeID == "" {
		return fmt.Errorf("no creatable issue type in %s", s.ctx.projectKey)
	}
	fields := map[string]any{
		"project":   map[string]any{"key": s.ctx.projectKey},
		"issuetype": map[string]any{"id": typeID},
		"summary":   summary,
	}
	if desc != "" {
		fields["description"] = client.TextToADF(desc)
	}
	ref, err := s.svc.Client.CreateIssue(c, fields)
	if err != nil {
		return err
	}
	fmt.Printf("created %s\n", ref.Key)
	return nil
}

// sprint handles `sprint add [key|id ...]`: adds the given issues (or, with no
// keys, the current issue) to the project's active sprint.
func (s *Session) sprint(args []string) error {
	if s.ctx.projectKey == "" || s.inWiki() {
		return fmt.Errorf("sprint works in a project or issue context")
	}
	if len(args) == 0 || args[0] != "add" {
		return fmt.Errorf("usage: sprint add [key|id ...]")
	}
	var keys []string
	for _, a := range args[1:] {
		keys = append(keys, s.normalizeIssueKey(a))
	}
	if len(keys) == 0 {
		if s.ctx.issueKey == "" {
			return fmt.Errorf("usage: sprint add <key> ... (or run inside an issue)")
		}
		keys = []string{s.ctx.issueKey}
	}
	sp := wizard.ActiveSprint(s.svc, s.ctx.projectKey)
	if sp == nil {
		return fmt.Errorf("no active sprint for %s", s.ctx.projectKey)
	}
	c, cancel := s.c()
	defer cancel()
	if err := s.svc.Client.AddIssuesToSprint(c, sp.ID, keys...); err != nil {
		return err
	}
	fmt.Printf("added %d issue(s) to %s\n", len(keys), sp.Name)
	return nil
}

func (s *Session) open(args []string) error {
	c, cancel := s.c()
	defer cancel()
	if s.inWiki() {
		id, isFolder, err := s.wikiTarget(c, args)
		if err != nil {
			return err
		}
		if isFolder {
			raw, err := s.svc.Client.FolderRaw(c, id)
			if err != nil {
				return err
			}
			var f struct {
				Links struct {
					WebUI string `json:"webui"`
				} `json:"_links"`
			}
			if err := json.Unmarshal(raw, &f); err != nil {
				return err
			}
			return openBrowser(s.svc.Client.WikiURL(f.Links.WebUI))
		}
		pg, err := s.svc.Client.Page(c, id, false)
		if err != nil {
			return err
		}
		return openBrowser(s.svc.Client.WikiURL(pg.Links.WebUI))
	}
	key, err := s.issueArg(args)
	if err != nil {
		return err
	}
	return openBrowser(s.svc.Client.IssueURL(key))
}

func (s *Session) trace(args []string) error {
	key, depth, dir, err := parseTraceArgs(args, s.ctx.issueKey, s.ctx.projectKey)
	if err != nil {
		return err
	}
	c, cancel := s.c()
	defer cancel()
	root, err := s.svc.Trace(c, key, service.TraceOptions{Depth: depth, Direction: dir})
	if err != nil {
		return err
	}
	s.p.Trace(root)
	return nil
}

// parseTraceArgs interprets `trace [key] [depth] [up|down|both]` with the
// arguments in any order; the key falls back to the context issue.
func parseTraceArgs(args []string, ctxKey, projectKey string) (key string, depth int, dir string, err error) {
	key, depth, dir = ctxKey, 2, "both"
	for _, a := range args {
		al := strings.ToLower(a)
		switch {
		case al == "up" || al == "down" || al == "both" || al == "inward" || al == "outward":
			dir = al
		case isDigits(a):
			// A bare number is a depth - unless a project context lets it
			// complete to an issue key and no key was given yet.
			var d int
			fmt.Sscanf(a, "%d", &d)
			if d > 0 && d <= 10 {
				depth = d
			} else if projectKey != "" && key == ctxKey {
				key = projectKey + "-" + a
			} else {
				depth = d
			}
		case looksLikeIssueKey(a):
			key = strings.ToUpper(a)
		default:
			return "", 0, "", fmt.Errorf("unknown trace argument %q (usage: trace [key] [depth] [up|down|both])", a)
		}
	}
	if key == "" {
		return "", 0, "", fmt.Errorf("no issue in context (trace <key>)")
	}
	return key, depth, dir, nil
}

// --- small helpers ---

// looksLikeIssueKey matches PROJ-123 style tokens (or bare numbers when a
// project context exists - those are normalised elsewhere).
func looksLikeIssueKey(s string) bool {
	i := strings.IndexByte(s, '-')
	if i <= 0 || i == len(s)-1 {
		return false
	}
	for _, r := range s[i+1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isDigits reports whether s is a non-empty numeric string.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// normalizeIssueKey turns an issue reference into a full key. A dashless token
// (typically a bare number like "842") is prefixed with the current project
// key, so inside a project context tickets can be referenced by id alone.
func (s *Session) normalizeIssueKey(tok string) string {
	key := strings.ToUpper(tok)
	if !strings.Contains(key, "-") && s.ctx.projectKey != "" {
		key = s.ctx.projectKey + "-" + key
	}
	return key
}

// isIssueKeyToken reports whether tok should be treated as an explicit issue
// key argument: a full PROJ-123 key, or a bare number inside a project context
// (which normalizeIssueKey expands to <project>-<n>).
func (s *Session) isIssueKeyToken(tok string) bool {
	return looksLikeIssueKey(tok) || (isDigits(tok) && s.ctx.projectKey != "")
}
