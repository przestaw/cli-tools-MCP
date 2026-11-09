// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package issue

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/prompt"
)

const usage = `issue commands (alias: i):
  issue [show] KEY [--include comments,transitions] [--json|--raw]
  issue search|find [--max N] [--start N] [--page-token T] [--json] JQL
  issue update KEY [--transition T] [--resolve] [--resolution R] [--fix V,..] [--assignee U] [--comment TEXT] [--link TYPE=KEY]
  issue move KEY TRANSITION      issue close KEY [--as RESOLUTION]      issue dup KEY OTHER
  issue link KEY TYPE OTHER      issue comment KEY TEXT
writes accept --dry-run (show the steps) and --yes (no confirmation); --json prints the common JSON shape`

// in, out and isTerminal are swapped by tests.
var (
	in         io.Reader = os.Stdin
	out        io.Writer = os.Stdout
	isTerminal           = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
)

// Command runs `workctl issue ...`.
func Command(cfg *config.Config, args []string) (resultErr error) {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		_, err := fmt.Fprintln(out, usage)
		return err
	}
	sub := args[0]
	switch sub {
	case "show", "search", "find", "update", "move", "close", "dup", "link", "comment":
		args = args[1:]
	default:
		sub = "show"
	}
	fs := flag.NewFlagSet("issue "+sub, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	jsonOut := fs.Bool("json", false, "")
	var (
		include                                        = fs.String("include", "", "")
		raw                                            = fs.Bool("raw", false, "")
		max                                            = fs.Int("max", 50, "")
		start                                          = fs.Int("start", 0, "")
		pageToken                                      = fs.String("page-token", "", "")
		transition, resolution, fix, assignee, comment = fs.String("transition", "", ""), fs.String("resolution", "", ""), fs.String("fix", "", ""), fs.String("assignee", "", ""), fs.String("comment", "", "")
		as, link                                       = fs.String("as", "", ""), fs.String("link", "", "")
		resolve, dryRun, yes                           = fs.Bool("resolve", false, ""), fs.Bool("dry-run", false, ""), fs.Bool("yes", false, "")
	)
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return fmt.Errorf("%w\n%s", err, usage)
	}
	need := func(n int, form string) error {
		if len(pos) != n {
			return fmt.Errorf("usage: workctl issue %s", form)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.CommandTimeout(kindFor(cfg)))
	defer cancel()

	if sub == "search" || sub == "find" {
		if len(pos) == 0 {
			return fmt.Errorf("usage: workctl issue search JQL")
		}
		b, err := Open(cfg, "", "")
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, b.Close()) }()
		r, err := b.Search(ctx, strings.Join(pos, " "), *start, *max, *pageToken)
		if err != nil {
			return err
		}
		if *jsonOut {
			return printJSON(r)
		}
		printSearch(r)
		return nil
	}

	var key string
	u := Update{Transition: *transition, Resolve: *resolve, Resolution: *resolution, FixVersions: cliutil.SplitCommaList(*fix),
		Assignee: *assignee, Comment: *comment}
	if *link != "" {
		t, k, ok := strings.Cut(*link, "=")
		if !ok {
			return fmt.Errorf("--link wants TYPE=KEY, for example duplicates=ABC-1")
		}
		u.Link = &LinkSpec{Type: t, Key: k}
	}
	switch sub {
	case "show":
		if err := need(1, "show KEY"); err != nil {
			return err
		}
		key = pos[0]
	case "update":
		if err := need(1, "update KEY [flags]"); err != nil {
			return err
		}
		key = pos[0]
	case "move":
		if err := need(2, "move KEY TRANSITION"); err != nil {
			return err
		}
		key, u.Transition = pos[0], pos[1]
	case "close":
		if err := need(1, "close KEY [--as RESOLUTION] [--fix V] [--comment TEXT]"); err != nil {
			return err
		}
		key, u.Resolve = pos[0], true
		u.Resolution = cliutil.FirstNonEmpty(*as, *resolution, "Done")
	case "dup":
		if err := need(2, "dup KEY OTHER [--fix V] [--comment TEXT]"); err != nil {
			return err
		}
		key, u.Resolve, u.Resolution = pos[0], true, cliutil.FirstNonEmpty(*as, *resolution, "Duplicate")
		u.Link = &LinkSpec{Type: "duplicates", Key: pos[1]}
	case "link":
		if len(pos) < 3 {
			return fmt.Errorf("usage: workctl issue link KEY TYPE OTHER (TYPE may be several words)")
		}
		key = pos[0]
		u.Link = &LinkSpec{Type: strings.Join(pos[1:len(pos)-1], " "), Key: pos[len(pos)-1]}
	case "comment":
		if len(pos) < 2 {
			return fmt.Errorf("usage: workctl issue comment KEY TEXT")
		}
		key, u.Comment = pos[0], strings.Join(pos[1:], " ")
	}

	b, err := Open(cfg, "", key)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, b.Close()) }()

	if sub == "show" {
		parts, err := ParseInclude(cliutil.SplitCommaList(*include))
		if err != nil {
			return err
		}
		if *raw {
			data, err := b.ShowRaw(ctx, key, parts)
			if err != nil {
				return err
			}
			return cliutil.PrintBody(data)
		}
		is, err := b.Show(ctx, key, parts)
		if err != nil {
			return err
		}
		if *jsonOut {
			return printJSON(is)
		}
		printIssue(is)
		return nil
	}

	if !*dryRun {
		if err := cfg.RequireWrite(); err != nil {
			return err
		}
	}
	steps, err := b.Plan(ctx, key, u)
	if err != nil {
		return err
	}
	if *dryRun || !*yes {
		for _, s := range steps {
			fmt.Fprintf(out, "  %s\n", s.Describe)
		}
	}
	if *dryRun {
		return nil
	}
	if !*yes {
		if !isTerminal() {
			return fmt.Errorf("not a terminal: pass --yes to apply")
		}
		if !prompt.AskBool(bufio.NewReader(in), "Apply to "+key+"?", false) {
			return fmt.Errorf("cancelled")
		}
	}
	if _, err := b.Apply(ctx, steps); err != nil {
		return err
	}
	is, err := b.Show(ctx, key, Include{})
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(is)
	}
	printIssue(is)
	return nil
}

// kindFor is the kind whose timeout bounds an issue command.
func kindFor(cfg *config.Config) string {
	if p, ok := cfg.Profiles[cfg.ProfileName()]; ok && isIssueKind(p.Kind) {
		return p.Kind
	}
	if !cfg.Has("jira") && cfg.Has("atlassian") {
		return "atlassian"
	}
	return "jira"
}

// parseInterspersed accepts flags before and after the positional arguments (`close KEY --as Duplicate`).
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func printJSON(v any) error {
	data, err := marshal(v, nil)
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	return err
}

func printIssue(is *Issue) {
	status := is.Status
	if is.Resolution != "" {
		status += " (" + is.Resolution + ")"
	}
	fmt.Fprintf(out, "%s  %s  %s\n%s\n", is.Key, status, is.Type, is.Title)
	var meta []string
	for _, kv := range [][2]string{{"assignee", is.Assignee}, {"fix", strings.Join(is.FixVersions, ", ")}, {"updated", is.Updated}} {
		if kv[1] != "" {
			meta = append(meta, kv[0]+": "+kv[1])
		}
	}
	if len(meta) > 0 {
		fmt.Fprintln(out, strings.Join(meta, "   "))
	}
	for _, l := range is.Links {
		fmt.Fprintf(out, "  %s %s  %s  %s\n", l.Relation, l.Key, l.Status, l.Title)
	}
	for _, t := range is.Transitions {
		fmt.Fprintf(out, "  -> %s %q to %s\n", t.ID, t.Name, t.To)
	}
	for _, c := range is.Comments {
		fmt.Fprintf(out, "--- %s %s\n%s\n", c.Author, c.Created, c.Body)
	}
	fmt.Fprintln(out, is.URL)
}

func printSearch(r *SearchResult) {
	for _, is := range r.Issues {
		fmt.Fprintf(out, "%-14s %-18s %s\n", is.Key, is.Status, is.Title)
	}
	switch {
	case r.NextPageToken != "":
		fmt.Fprintf(out, "(more: --page-token %s)\n", r.NextPageToken)
	case r.Total > r.StartAt+len(r.Issues):
		fmt.Fprintf(out, "(%d of %d: --start %d)\n", len(r.Issues), r.Total, r.StartAt+len(r.Issues))
	}
}
