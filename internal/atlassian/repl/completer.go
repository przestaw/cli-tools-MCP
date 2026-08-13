package repl

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/c-bata/go-prompt"
)

// topCommands are the REPL verbs offered by autocomplete.
var topCommands = []prompt.Suggest{
	{Text: "ls", Description: "list current context; `ls more` / `ls all` continue long listings"},
	{Text: "cd", Description: "navigate: cd <key|id> | cd jira | cd wiki | cd .. | cd /"},
	{Text: "use", Description: "use project <key> | use space <key>"},
	{Text: "pwd", Description: "print current context path"},
	{Text: "get", Description: "show issue/page (current if no arg)"},
	{Text: "jql", Description: "run a JQL query"},
	{Text: "cql", Description: "run a Confluence CQL query"},
	{Text: "find", Description: "find issues/pages by text in current context"},
	{Text: "mywork", Description: "open issues assigned to me"},
	{Text: "trace", Description: "traceability tree: trace [key] [depth] [up|down|both]"},
	{Text: "comment", Description: "comment list|add [key|id] <text>"},
	{Text: "create", Description: "create (interactive form) | create <summary> [:: <description>] (project context)"},
	{Text: "sprint", Description: "sprint add [key|id ...] - add issues (or current) to the project's active sprint"},
	{Text: "set-field", Description: "set-field [key|id] <field> <value>"},
	{Text: "transition", Description: "transition [key|id] <status>"},
	{Text: "open", Description: "open issue/page in browser"},
	{Text: "expand", Description: "toggle full (untruncated) value display"},
	{Text: "help", Description: "show command help"},
	{Text: "exit", Description: "quit the shell"},
}

// complete provides command + context-aware argument completion.
func (s *Session) complete(d prompt.Document) []prompt.Suggest {
	word := d.GetWordBeforeCursor()
	if strings.Contains(strings.TrimRight(d.TextBeforeCursor(), word), " ") {
		first := strings.Fields(d.TextBeforeCursor())
		if len(first) >= 1 {
			switch first[0] {
			case "use":
				return prompt.FilterHasPrefix([]prompt.Suggest{
					{Text: "project"}, {Text: "space"},
				}, word, true)
			case "comment":
				return prompt.FilterHasPrefix([]prompt.Suggest{
					{Text: "list"}, {Text: "add"},
				}, word, true)
			case "sprint":
				return prompt.FilterHasPrefix([]prompt.Suggest{
					{Text: "add"},
				}, word, true)
			case "cd":
				sug := []prompt.Suggest{{Text: ".."}, {Text: "/"}}
				sug = append(sug, s.navSuggestions()...)
				return prompt.FilterHasPrefix(sug, word, true)
			case "ls":
				if len(first) >= 2 && first[len(first)-1] == "by" ||
					len(first) >= 3 && first[len(first)-2] == "by" && word != "" {
					return prompt.FilterHasPrefix(s.sortFieldSuggestions(), word, true)
				}
				return prompt.FilterHasPrefix([]prompt.Suggest{
					{Text: "more", Description: "next page of the last listing"},
					{Text: "all", Description: "fetch everything (capped)"},
					{Text: "by", Description: "sort: ls by <field> [asc|desc]"},
					{Text: "asc"}, {Text: "desc"},
				}, word, true)
			case "get", "open", "trace", "transition", "set-field":
				return prompt.FilterHasPrefix(s.entrySuggestions(), word, true)
			}
		}
		return nil
	}
	return prompt.FilterHasPrefix(topCommands, word, true)
}

// sortFieldSuggestions returns the sort fields valid in the current context.
func (s *Session) sortFieldSuggestions() []prompt.Suggest {
	var fields []string
	switch s.ctx.level {
	case levelProject:
		fields = []string{"updated", "created", "key", "priority", "status", "summary", "duedate", "rank"}
	case levelSpace:
		fields = []string{"title", "modified", "created", "id"}
	case levelPage:
		fields = []string{"title"}
	case levelJira, levelWiki:
		fields = []string{"key", "name"}
	case levelIssue:
		fields = []string{"key", "summary", "status"}
	}
	out := make([]prompt.Suggest, 0, len(fields))
	for _, f := range fields {
		out = append(out, prompt.Suggest{Text: f})
	}
	return out
}

// navSuggestions returns cd targets for the current level.
func (s *Session) navSuggestions() []prompt.Suggest {
	switch s.ctx.level {
	case levelRoot:
		out := []prompt.Suggest{
			{Text: "jira", Description: "Jira projects"},
			{Text: "wiki", Description: "Confluence spaces"},
		}
		for _, p := range s.projects {
			out = append(out, prompt.Suggest{Text: p.Key, Description: p.Name})
		}
		return out
	case levelJira:
		out := make([]prompt.Suggest, 0, len(s.projects))
		for _, p := range s.projects {
			out = append(out, prompt.Suggest{Text: p.Key, Description: p.Name})
		}
		return out
	case levelWiki:
		out := make([]prompt.Suggest, 0, len(s.spaces))
		for _, sp := range s.spaces {
			out = append(out, prompt.Suggest{Text: sp.Key, Description: sp.Name})
		}
		return out
	default:
		return s.entrySuggestions()
	}
}

// entrySuggestions returns the cached issues/pages of the current context.
func (s *Session) entrySuggestions() []prompt.Suggest {
	if s.inWiki() {
		out := make([]prompt.Suggest, 0, len(s.entries))
		for _, e := range s.entries {
			out = append(out, prompt.Suggest{Text: e.ID, Description: e.Type + ": " + e.Title})
		}
		return out
	}
	out := make([]prompt.Suggest, 0, len(s.issues))
	for _, is := range s.issues {
		out = append(out, prompt.Suggest{Text: is.Key, Description: is.Fields.Summary})
	}
	return out
}

// printHelp lists the REPL commands.
func printHelp() {
	fmt.Println(`inside a Jira project, tickets can be referenced by bare id (e.g. 842 = <PROJECT>-842)
in cd/get/open/comment/transition/set-field/trace; full PROJ-123 keys work anywhere.

commands:
  ls                        list current context (root: jira + wiki scopes; jira: projects;
                            project: recent issues; space: root folders + pages;
                            folder/page: children)
  ls more | ls all          continue the last listing (next page / everything, capped)
  ls by <field> [asc|desc]  sort: issues by updated|created|key|priority|status|summary|
                            duedate|rank; space pages by title|modified|created|id;
                            children/projects/spaces/subtasks client-side (title/key/name/...)
  cd <key|id> | .. | /      navigate (cd jira / cd wiki pick the scope; project keys also
                            work directly at root; bare numbers in a project are
                            completed to <PROJECT>-<n>)
  use project <key>         jump to a Jira project
  use space <key>           jump to a Confluence space
  pwd                       show context path
  get [key|id|pageId]       show an issue (or page in wiki context)
  jql <query>               run a JQL query (e.g. project = AB AND status = Open)
  cql <query>               run a CQL query (e.g. space = AB AND title ~ 'design')
  find <text>               find issues (or pages) by text in the current context
  mywork                    open issues assigned to me
  trace [key] [depth] [dir] link tree (dir: up|down|both; key defaults to the
                            current issue; args in any order)
  comment list [key|id]     list comments (issue or page)
  comment add [key|id] <text>  add a comment (issue or page)
  create                    interactive create: pick issue type, fill every required field
  create <summary> [:: <d>] quick-create an issue in the current project (type defaults to Task)
  sprint add [key|id ...]   add issues (or the current issue) to the project's active sprint
  set-field [key|id] <f> <v>   set a field value (editmeta-coerced)
  transition [key|id] <status> change workflow status
  open [key|id|pageId]      open in browser
  expand                    toggle full (untruncated) value display
  help                      this help
  exit | quit               leave the shell`)
}

// stop terminates the shell. go-prompt does not expose a clean stop, so exit.
func stop() {
	os.Exit(0)
}

// openBrowser opens a URL in the default browser cross-platform.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
