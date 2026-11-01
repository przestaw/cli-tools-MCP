package app

import (
	"context"
	"encoding/json"
	"strings"

	"workctl/internal/api"
	"workctl/internal/config"
	"workctl/internal/gitlab"
	"workctl/internal/jira"
)

type taskProjectContext struct {
	Project  string          `json:"project"`
	Branches json.RawMessage `json:"branches,omitempty"`
	MRs      json.RawMessage `json:"merge_requests,omitempty"`
	Error    string          `json:"error,omitempty"`
}

type taskContext struct {
	Key    string               `json:"key"`
	Jira   json.RawMessage      `json:"jira,omitempty"`
	GitLab []taskProjectContext `json:"gitlab,omitempty"`
	Errors []string             `json:"errors,omitempty"`
}

// taskContextJSON collects the Jira issue and the matching GitLab branches and merge requests of the default profiles.
func taskContextJSON(ctx context.Context, cfg *config.Config, key string) ([]byte, error) {
	jc, err := api.NewKindClient(cfg, "jira")
	if err != nil {
		return nil, err
	}
	gc, err := api.NewKindClient(cfg, "gitlab")
	if err != nil {
		return nil, err
	}
	out := taskContext{Key: key}
	issue, err := jira.GetIssue(ctx, jc, key, nil)
	if err != nil {
		out.Errors = append(out.Errors, "jira: "+err.Error())
	} else {
		out.Jira = json.RawMessage(issue)
	}
	_, profile, _ := cfg.ProfileOf("gitlab")
	projects := profile.Projects
	if len(projects) == 0 && profile.DefaultProject != "" {
		projects = []string{profile.DefaultProject}
	}
	for _, project := range projects {
		pc := taskProjectContext{Project: project}
		branches, berr := gitlab.ListBranches(ctx, gc, project, key)
		mrs, merr := gitlab.ListMRs(ctx, gc, project, key, "all")
		if berr != nil || merr != nil {
			var parts []string
			if berr != nil {
				parts = append(parts, "branches: "+berr.Error())
			}
			if merr != nil {
				parts = append(parts, "MRs: "+merr.Error())
			}
			pc.Error = strings.Join(parts, "; ")
		}
		if berr == nil {
			pc.Branches = json.RawMessage(branches)
		}
		if merr == nil {
			pc.MRs = json.RawMessage(mrs)
		}
		out.GitLab = append(out.GitLab, pc)
	}
	return json.MarshalIndent(out, "", "  ")
}
