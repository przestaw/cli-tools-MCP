// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"workctl/internal/azure/client"
	"workctl/internal/azure/diff"
	"workctl/internal/azure/output"
	"workctl/internal/azure/service"
)

var prCmd = &cobra.Command{
	Use:     "pr",
	Aliases: []string{"pullrequest", "pull"},
	Short:   "Pull requests",
}

// pr flags
var (
	prRepo        string
	prStatus      string
	prMine        bool
	prCreator     string
	prReviewer    string
	prSource      string
	prTarget      string
	prTitle       string
	prDescription string
	prDraft       bool
	prReviewers   []string
	prSquash      bool
	prDeleteSrc   bool
	prMergeMsg    string
	prDraftSet    string
	prIteration   int
	prDiffContext int
	prDiffPath    string
	prCommentFile string
	prCommentLine int
	prCommentEnd  int
	prCommentCol  int
	prCommentSide string
)

// splitLines splits file bytes into lines for diffing, normalizing CRLF and
// dropping a single trailing newline so the last line isn't a phantom empty one.
func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// isBinary reports whether data looks binary (contains a NUL byte).
func isBinary(b []byte) bool {
	return bytes.IndexByte(b, 0) >= 0
}

var prListCmd = &cobra.Command{
	Use:   "list [--repo R] [--status active|completed|abandoned|all] [--mine]",
	Short: "List pull requests",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		search := client.PullRequestSearch{
			Status:       prStatus,
			CreatorID:    prCreator,
			ReviewerID:   prReviewer,
			Sourcebranch: service.NormalizeBranch(prSource),
			TargetBranch: service.NormalizeBranch(prTarget),
			Top:          flagTop,
		}
		if prMine {
			me, err := svc.CurrentUserID(c)
			if err != nil {
				return err
			}
			search.CreatorID = me
		}
		prs, err := svc.Client.PullRequests(c, project, prRepo, search, flagAll)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(prs)
		}
		rows := make([][]string, len(prs))
		for i, pr := range prs {
			rows[i] = []string{
				fmt.Sprintf("%d", pr.PullRequestID),
				pr.Title,
				prStatusLabel(pr),
				pr.Repository.Name,
				service.ShortRef(pr.SourceRefName) + " -> " + service.ShortRef(pr.TargetRefName),
				output.UserName(pr.CreatedBy),
			}
		}
		return p.Table([]string{"ID", "Title", "Status", "Repo", "Branches", "Author"}, rows)
	},
}

var prGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a pull request",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("pr id", args[0])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		pr, err := svc.Client.PullRequest(c, project, id)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.Raw(pr.Raw)
		}
		return p.KeyValue([][2]string{
			{"ID", fmt.Sprintf("%d", pr.PullRequestID)},
			{"Title", pr.Title},
			{"Status", prStatusLabel(*pr)},
			{"Repo", pr.Repository.Name},
			{"Source", service.ShortRef(pr.SourceRefName)},
			{"Target", service.ShortRef(pr.TargetRefName)},
			{"Author", output.UserName(pr.CreatedBy)},
			{"Mergeability", pr.MergeStatus},
			{"Created", shortTime(pr.CreationDate)},
			{"Reviewers", reviewerSummary(pr.Reviewers)},
			{"Description", pr.Description},
		})
	},
}

var prCreateCmd = &cobra.Command{
	Use:   "create --repo R --source B --target B --title T",
	Short: "Open a pull request",
	RunE: func(cmd *cobra.Command, args []string) error {
		if prRepo == "" || prSource == "" || prTarget == "" || prTitle == "" {
			return fmt.Errorf("--repo, --source, --target and --title are required")
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		body := map[string]any{
			"sourceRefName": service.NormalizeBranch(prSource),
			"targetRefName": service.NormalizeBranch(prTarget),
			"title":         prTitle,
			"isDraft":       prDraft,
		}
		if prDescription != "" {
			body["description"] = prDescription
		}
		if len(prReviewers) > 0 {
			var revs []map[string]any
			for _, r := range prReviewers {
				revs = append(revs, map[string]any{"id": r})
			}
			body["reviewers"] = revs
		}
		pr, err := svc.Client.CreatePullRequest(c, project, prRepo, body)
		if err != nil {
			return err
		}
		printer().Msg("created PR !%d", pr.PullRequestID)
		return nil
	},
}

var prUpdateCmd = &cobra.Command{
	Use:   "update <id> [--title T] [--description D] [--draft true|false]",
	Short: "Update a pull request's title/description/draft state",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("pr id", args[0])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		repoID, err := resolvePRRepoID(c, svc, project, id)
		if err != nil {
			return err
		}
		body := map[string]any{}
		if prTitle != "" {
			body["title"] = prTitle
		}
		if prDescription != "" {
			body["description"] = prDescription
		}
		if prDraftSet != "" {
			body["isDraft"] = prDraftSet == "true" || prDraftSet == "1" || prDraftSet == "yes"
		}
		if len(body) == 0 {
			return fmt.Errorf("nothing to update (use --title/--description/--draft)")
		}
		if _, err := svc.Client.UpdatePullRequest(c, project, repoID, id, body); err != nil {
			return err
		}
		printer().Msg("updated PR !%d", id)
		return nil
	},
}

var prCompleteCmd = &cobra.Command{
	Use:   "complete <id> [--squash] [--delete-source]",
	Short: "Complete (merge) a pull request",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("pr id", args[0])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		pr, err := svc.Client.PullRequest(c, project, id)
		if err != nil {
			return err
		}
		completion, strategy := completionOptions(pr)
		body := map[string]any{
			"status":            "completed",
			"completionOptions": completion,
		}
		if pr.LastMergeSourceCommit != nil {
			body["lastMergeSourceCommit"] = map[string]any{"commitId": pr.LastMergeSourceCommit.CommitID}
		}
		if _, err := svc.Client.UpdatePullRequest(c, project, pr.Repository.ID, id, body); err != nil {
			return err
		}
		printer().Msg("completing PR !%d (%s)", id, strategy)
		return nil
	},
}

var prAutoCompleteCmd = &cobra.Command{
	Use:     "autocomplete <id> [--squash] [--delete-source]",
	Aliases: []string{"auto-complete"},
	Short:   "Set auto-complete on a pull request (merges once all policies pass)",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("pr id", args[0])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		pr, err := svc.Client.PullRequest(c, project, id)
		if err != nil {
			return err
		}
		me, err := svc.CurrentUserID(c)
		if err != nil {
			return err
		}
		completion, strategy := completionOptions(pr)
		body := map[string]any{
			"autoCompleteSetBy": map[string]any{"id": me},
			"completionOptions": completion,
		}
		if _, err := svc.Client.UpdatePullRequest(c, project, pr.Repository.ID, id, body); err != nil {
			return err
		}
		printer().Msg("auto-complete set on PR !%d (%s)", id, strategy)
		return nil
	},
}

// completionOptions builds the completionOptions shared by complete and
// autocomplete. The merge commit message is always sent: without it the server
// falls back to "Merge pull request <id> from <src> into <dst>" and drops the
// title and description.
func completionOptions(pr *client.PullRequest) (map[string]any, string) {
	strategy := "noFastForward"
	if prSquash {
		strategy = "squash"
	}
	msg := prMergeMsg
	if msg == "" {
		msg = service.MergeCommitMessage(pr.PullRequestID, pr.Title, pr.Description)
	}
	return map[string]any{
		"deleteSourceBranch": prDeleteSrc,
		"mergeStrategy":      strategy,
		"mergeCommitMessage": msg,
	}, strategy
}

var prAbandonCmd = &cobra.Command{
	Use:   "abandon <id>",
	Short: "Abandon a pull request",
	Args:  cobra.ExactArgs(1),
	RunE:  prStatusSetter("abandoned"),
}

var prReactivateCmd = &cobra.Command{
	Use:   "reactivate <id>",
	Short: "Reactivate an abandoned pull request",
	Args:  cobra.ExactArgs(1),
	RunE:  prStatusSetter("active"),
}

// prStatusSetter returns a RunE that PATCHes a PR to the given status.
func prStatusSetter(status string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("pr id", args[0])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		repoID, err := resolvePRRepoID(c, svc, project, id)
		if err != nil {
			return err
		}
		if _, err := svc.Client.UpdatePullRequest(c, project, repoID, id, map[string]any{"status": status}); err != nil {
			return err
		}
		printer().Msg("PR !%d -> %s", id, status)
		return nil
	}
}

var prVoteCmd = &cobra.Command{
	Use:   "vote <id> <approve|approve-suggestions|reset|wait|reject>",
	Short: "Cast your review vote on a pull request",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("pr id", args[0])
		if err != nil {
			return err
		}
		vote, err := service.ParseVote(args[1])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		repoID, err := resolvePRRepoID(c, svc, project, id)
		if err != nil {
			return err
		}
		me, err := svc.CurrentUserID(c)
		if err != nil {
			return err
		}
		if err := svc.Client.SetReviewerVote(c, project, repoID, id, me, vote); err != nil {
			return err
		}
		printer().Msg("voted %q on PR !%d", service.VoteName(vote), id)
		return nil
	},
}

var prThreadsCmd = &cobra.Command{
	Use:     "threads <id>",
	Aliases: []string{"comments"},
	Short:   "List the comment threads of a pull request",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("pr id", args[0])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		repoID, err := resolvePRRepoID(c, svc, project, id)
		if err != nil {
			return err
		}
		threads, err := svc.Client.PRThreads(c, project, repoID, id)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(threads)
		}
		var rows [][]string
		for _, t := range threads {
			if t.IsDeleted {
				continue
			}
			for _, cm := range t.Comments {
				if cm.Content == "" {
					continue
				}
				rows = append(rows, []string{
					fmt.Sprintf("%d", t.ID), output.UserName(cm.Author), t.Status, cm.Content,
				})
			}
		}
		return p.Table([]string{"Thread", "Author", "Status", "Comment"}, rows)
	},
}

var prCommentCmd = &cobra.Command{
	Use:   "comment <id> <text>",
	Short: "Add a comment to a pull request (top-level, or inline with --file/--line)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("pr id", args[0])
		if err != nil {
			return err
		}
		tc, err := commentThreadContext()
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		repoID, err := resolvePRRepoID(c, svc, project, id)
		if err != nil {
			return err
		}
		th, err := svc.Client.AddPRComment(c, project, repoID, id, args[1], tc)
		if err != nil {
			return err
		}
		if tc != nil {
			printer().Msg("added comment (thread %d) on %s:%d", th.ID, strings.TrimPrefix(tc.FilePath, "/"), prCommentLine)
		} else {
			printer().Msg("added comment (thread %d)", th.ID)
		}
		return nil
	},
}

// commentThreadContext builds the inline anchor for `pr comment` from the
// --file/--line/--end-line/--column/--side flags, or returns nil for a
// top-level comment when --file is not set.
func commentThreadContext() (*client.ThreadContext, error) {
	if prCommentFile == "" {
		if prCommentLine != 0 {
			return nil, fmt.Errorf("--line requires --file")
		}
		return nil, nil
	}
	if prCommentLine <= 0 {
		return nil, fmt.Errorf("--file requires --line (the 1-based line to anchor to)")
	}
	end := prCommentEnd
	if end <= 0 {
		end = prCommentLine
	}
	if end < prCommentLine {
		return nil, fmt.Errorf("--end-line (%d) must be >= --line (%d)", end, prCommentLine)
	}
	col := prCommentCol
	if col <= 0 {
		col = 1
	}
	path := prCommentFile
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	start := &client.ThreadPosition{Line: prCommentLine, Offset: col}
	stop := &client.ThreadPosition{Line: end, Offset: col}
	tc := &client.ThreadContext{FilePath: path}
	switch strings.ToLower(prCommentSide) {
	case "", "right", "new":
		tc.RightFileStart, tc.RightFileEnd = start, stop
	case "left", "old":
		tc.LeftFileStart, tc.LeftFileEnd = start, stop
	default:
		return nil, fmt.Errorf("--side must be right|left (got %q)", prCommentSide)
	}
	return tc, nil
}

var prChangesCmd = &cobra.Command{
	Use:     "changes <id>",
	Aliases: []string{"files"},
	Short:   "List the files changed in a pull request",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("pr id", args[0])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		repoID, err := resolvePRRepoID(c, svc, project, id)
		if err != nil {
			return err
		}
		iter := prIteration
		if iter == 0 {
			iters, err := svc.Client.PRIterations(c, project, repoID, id)
			if err != nil {
				return err
			}
			if len(iters) == 0 {
				return fmt.Errorf("pull request !%d has no iterations", id)
			}
			iter = iters[len(iters)-1].ID
		}
		changes, err := svc.Client.PRChanges(c, project, repoID, id, iter, 0)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(changes)
		}
		var rows [][]string
		for _, ch := range changes {
			path := ch.Item.Path
			if ch.OriginalPath != "" && ch.OriginalPath != path {
				path = ch.OriginalPath + " -> " + path
			}
			rows = append(rows, []string{ch.ChangeType, path})
		}
		return p.Table([]string{"Change", "Path"}, rows)
	},
}

// filePatch is one file's diff (used for --json output of `pr diff`).
type filePatch struct {
	Path       string `json:"path"`
	ChangeType string `json:"changeType"`
	Binary     bool   `json:"binary,omitempty"`
	Patch      string `json:"patch,omitempty"`
}

var prDiffCmd = &cobra.Command{
	Use:   "diff <id>",
	Short: "Show the content diff of a pull request",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("pr id", args[0])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		repoID, err := resolvePRRepoID(c, svc, project, id)
		if err != nil {
			return err
		}
		iters, err := svc.Client.PRIterations(c, project, repoID, id)
		if err != nil {
			return err
		}
		if len(iters) == 0 {
			return fmt.Errorf("pull request !%d has no iterations", id)
		}
		it := iters[len(iters)-1]
		if prIteration > 0 {
			found := false
			for _, x := range iters {
				if x.ID == prIteration {
					it, found = x, true
					break
				}
			}
			if !found {
				return fmt.Errorf("pull request !%d has no iteration %d", id, prIteration)
			}
		}
		base, head := it.CommonRefCommit.CommitID, it.SourceRefCommit.CommitID
		changes, err := svc.Client.PRChanges(c, project, repoID, id, it.ID, 0)
		if err != nil {
			return err
		}
		var patches []filePatch
		for _, ch := range changes {
			if ch.Item.IsFolder || (ch.Item.GitObjectType != "" && ch.Item.GitObjectType != "blob") {
				continue // skip folders and submodule (commit) entries
			}
			path := ch.Item.Path
			if path == "" || (prDiffPath != "" && !strings.Contains(path, prDiffPath)) {
				continue
			}
			ct := strings.ToLower(ch.ChangeType)
			oldPath := path
			if ch.OriginalPath != "" {
				oldPath = ch.OriginalPath
			}
			var oldB, newB []byte
			if !strings.Contains(ct, "add") {
				if oldB, err = svc.Client.FileContentAtCommit(c, project, repoID, oldPath, base); err != nil {
					return err
				}
			}
			if !strings.Contains(ct, "delete") {
				if newB, err = svc.Client.FileContentAtCommit(c, project, repoID, path, head); err != nil {
					return err
				}
			}
			if len(oldB) == 0 && len(newB) == 0 {
				continue // no content on either side: submodule/gitlink or empty file
			}
			fp := filePatch{Path: path, ChangeType: ch.ChangeType}
			if isBinary(oldB) || isBinary(newB) {
				fp.Binary = true
			} else {
				fp.Patch = diff.Unified(splitLines(oldB), splitLines(newB), prDiffContext)
			}
			patches = append(patches, fp)
		}
		if p.JSON {
			return p.JSONValue(patches)
		}
		if len(patches) == 0 {
			p.Msg("no file changes")
			return nil
		}
		var out strings.Builder
		for _, fp := range patches {
			old := "a/" + strings.TrimPrefix(fp.Path, "/")
			neu := "b/" + strings.TrimPrefix(fp.Path, "/")
			fmt.Fprintf(&out, "diff --git %s %s\n", old, neu)
			switch {
			case strings.Contains(strings.ToLower(fp.ChangeType), "add"):
				out.WriteString("new file\n")
			case strings.Contains(strings.ToLower(fp.ChangeType), "delete"):
				out.WriteString("deleted file\n")
			case strings.Contains(strings.ToLower(fp.ChangeType), "rename"):
				out.WriteString("renamed (" + fp.ChangeType + ")\n")
			}
			switch {
			case fp.Binary:
				out.WriteString("Binary files differ\n")
			case fp.Patch == "":
				out.WriteString("(no textual changes)\n")
			default:
				out.WriteString(fp.Patch)
			}
			out.WriteString("\n")
		}
		p.Msg("%s", strings.TrimRight(out.String(), "\n"))
		return nil
	},
}

var prReplyCmd = &cobra.Command{
	Use:   "reply <id> <threadId> <text>",
	Short: "Reply to a comment thread on a pull request",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("pr id", args[0])
		if err != nil {
			return err
		}
		threadID, err := atoiArg("thread id", args[1])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		repoID, err := resolvePRRepoID(c, svc, project, id)
		if err != nil {
			return err
		}
		cm, err := svc.Client.AddPRThreadComment(c, project, repoID, id, threadID, 1, args[2])
		if err != nil {
			return err
		}
		printer().Msg("replied to thread %d (comment %d)", threadID, cm.ID)
		return nil
	},
}

var prResolveCmd = &cobra.Command{
	Use:   "resolve <id> <threadId>",
	Short: "Mark a comment thread as fixed (resolved)",
	Args:  cobra.ExactArgs(2),
	RunE:  prThreadStatusSetter("fixed", "resolved"),
}

var prReopenThreadCmd = &cobra.Command{
	Use:   "reopen <id> <threadId>",
	Short: "Reopen a resolved comment thread (set active)",
	Args:  cobra.ExactArgs(2),
	RunE:  prThreadStatusSetter("active", "reopened"),
}

// prThreadStatusSetter returns a RunE that PATCHes a PR thread's status.
func prThreadStatusSetter(status, verb string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("pr id", args[0])
		if err != nil {
			return err
		}
		threadID, err := atoiArg("thread id", args[1])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		repoID, err := resolvePRRepoID(c, svc, project, id)
		if err != nil {
			return err
		}
		if err := svc.Client.SetPRThreadStatus(c, project, repoID, id, threadID, status); err != nil {
			return err
		}
		printer().Msg("thread %d on PR !%d -> %s", threadID, id, verb)
		return nil
	}
}

var prReviewersCmd = &cobra.Command{
	Use:   "reviewers <id>",
	Short: "List the reviewers and their votes",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("pr id", args[0])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		pr, err := svc.Client.PullRequest(c, project, id)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(pr.Reviewers)
		}
		rows := make([][]string, len(pr.Reviewers))
		for i, r := range pr.Reviewers {
			required := ""
			if r.IsRequired {
				required = "required"
			}
			rows[i] = []string{r.DisplayName, service.VoteName(r.Vote), required}
		}
		return p.Table([]string{"Reviewer", "Vote", "Flags"}, rows)
	},
}

var prOpenCmd = &cobra.Command{
	Use:   "open <id>",
	Short: "Open a pull request in the browser",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("pr id", args[0])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		pr, err := svc.Client.PullRequest(c, project, id)
		if err != nil {
			return err
		}
		return openBrowser(svc.Client.PRWebURL(project, pr.Repository.Name, id))
	},
}

// resolvePRRepoID returns the repository id for repo-scoped PR operations.
// With --repo set it is used directly (saving one API call); otherwise the PR
// is fetched (project-scoped) and its repository id returned.
func resolvePRRepoID(c context.Context, svc *service.Service, project string, id int) (string, error) {
	if prRepo != "" {
		return prRepo, nil
	}
	pr, err := svc.Client.PullRequest(c, project, id)
	if err != nil {
		return "", err
	}
	return pr.Repository.ID, nil
}

// prStatusLabel renders a PR's lifecycle status, marking drafts and glossing
// completed PRs as merged. Distinct from mergeStatus, which only reports
// whether the PR can be merged, not whether it was.
func prStatusLabel(pr client.PullRequest) string {
	if pr.IsDraft && strings.EqualFold(pr.Status, "active") {
		return "draft"
	}
	if strings.EqualFold(pr.Status, "completed") {
		return "completed (merged)"
	}
	return pr.Status
}

// reviewerSummary renders reviewers as "Name(vote)" joined.
func reviewerSummary(revs []client.Reviewer) string {
	if len(revs) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(revs))
	for _, r := range revs {
		parts = append(parts, fmt.Sprintf("%s(%s)", r.DisplayName, service.VoteName(r.Vote)))
	}
	return strings.Join(parts, ", ")
}

func init() {
	prListCmd.Flags().StringVar(&prRepo, "repo", "", "limit to a repository (name or id)")
	prListCmd.Flags().StringVar(&prStatus, "status", "active", "active|completed|abandoned|all")
	prListCmd.Flags().BoolVar(&prMine, "mine", false, "only PRs you created")
	prListCmd.Flags().StringVar(&prCreator, "creator", "", "creator accountId")
	prListCmd.Flags().StringVar(&prReviewer, "reviewer", "", "reviewer accountId")
	prListCmd.Flags().StringVar(&prSource, "source", "", "source branch")
	prListCmd.Flags().StringVar(&prTarget, "target", "", "target branch")
	addListFlags(prListCmd, 50)

	prCreateCmd.Flags().StringVar(&prRepo, "repo", "", "repository (name or id) (required)")
	prCreateCmd.Flags().StringVar(&prSource, "source", "", "source branch (required)")
	prCreateCmd.Flags().StringVar(&prTarget, "target", "", "target branch (required)")
	prCreateCmd.Flags().StringVar(&prTitle, "title", "", "title (required)")
	prCreateCmd.Flags().StringVar(&prDescription, "description", "", "description")
	prCreateCmd.Flags().BoolVar(&prDraft, "draft", false, "create as draft")
	prCreateCmd.Flags().StringArrayVar(&prReviewers, "reviewer", nil, "reviewer accountId (repeatable)")

	prUpdateCmd.Flags().StringVar(&prTitle, "title", "", "new title")
	prUpdateCmd.Flags().StringVar(&prDescription, "description", "", "new description")
	prUpdateCmd.Flags().StringVar(&prDraftSet, "draft", "", "set draft state (true|false)")

	prCommentCmd.Flags().StringVar(&prCommentFile, "file", "", "anchor the comment inline to this file path (repo-relative)")
	prCommentCmd.Flags().IntVar(&prCommentLine, "line", 0, "1-based line to anchor to (required with --file)")
	prCommentCmd.Flags().IntVar(&prCommentEnd, "end-line", 0, "1-based end line for a multi-line anchor (default: --line)")
	prCommentCmd.Flags().IntVar(&prCommentCol, "column", 0, "1-based column offset for the anchor (default: 1)")
	prCommentCmd.Flags().StringVar(&prCommentSide, "side", "right", "which side of the diff to anchor to: right (new) or left (old)")

	prChangesCmd.Flags().IntVar(&prIteration, "iteration", 0, "iteration to list (0 = latest, i.e. the whole PR)")
	prDiffCmd.Flags().IntVar(&prIteration, "iteration", 0, "iteration to diff (0 = latest, i.e. the whole PR)")
	prDiffCmd.Flags().IntVar(&prDiffContext, "context", 3, "lines of context around each change")
	prDiffCmd.Flags().StringVar(&prDiffPath, "path", "", "only diff files whose path contains this substring")

	// Repo-scoped by-id commands accept an optional --repo to skip the PR lookup.
	for _, cmd := range []*cobra.Command{prThreadsCmd, prCommentCmd, prVoteCmd, prUpdateCmd, prAbandonCmd, prReactivateCmd,
		prChangesCmd, prDiffCmd, prReplyCmd, prResolveCmd, prReopenThreadCmd} {
		cmd.Flags().StringVar(&prRepo, "repo", "", "repository (name or id); skips the PR lookup")
	}

	for _, cmd := range []*cobra.Command{prCompleteCmd, prAutoCompleteCmd} {
		cmd.Flags().BoolVar(&prSquash, "squash", false, "squash merge")
		cmd.Flags().BoolVar(&prDeleteSrc, "delete-source", false, "delete source branch after merge")
		cmd.Flags().StringVar(&prMergeMsg, "merge-message", "", "merge commit message (default: Merged PR <id>: <title> + description)")
	}

	prCmd.AddCommand(prListCmd, prGetCmd, prCreateCmd, prUpdateCmd, prCompleteCmd, prAutoCompleteCmd,
		prAbandonCmd, prReactivateCmd, prVoteCmd, prThreadsCmd, prCommentCmd,
		prReviewersCmd, prOpenCmd, prChangesCmd, prDiffCmd, prReplyCmd, prResolveCmd, prReopenThreadCmd)
	rootCmd.AddCommand(prCmd)
}
