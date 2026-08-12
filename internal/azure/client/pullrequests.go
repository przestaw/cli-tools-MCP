package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// PullRequestSearch are the optional filters for listing pull requests.
type PullRequestSearch struct {
	Status       string // active|completed|abandoned|all (default active)
	CreatorID    string // accountId
	ReviewerID   string // accountId
	Sourcebranch string // refs/heads/...
	TargetBranch string // refs/heads/...
	Top          int
}

// PullRequests lists pull requests in a repository (or project-wide if repo == "").
func (c *Client) PullRequests(ctx context.Context, project, repo string, s PullRequestSearch, all bool) ([]PullRequest, error) {
	apiPath := "git/pullrequests"
	if repo != "" {
		apiPath = "git/repositories/" + url.PathEscape(repo) + "/pullrequests"
	}
	return getList[PullRequest](ctx, c, func(token string) string {
		q := url.Values{}
		if s.Status != "" {
			q.Set("searchCriteria.status", s.Status)
		}
		if s.CreatorID != "" {
			q.Set("searchCriteria.creatorId", s.CreatorID)
		}
		if s.ReviewerID != "" {
			q.Set("searchCriteria.reviewerId", s.ReviewerID)
		}
		if s.Sourcebranch != "" {
			q.Set("searchCriteria.sourceRefName", s.Sourcebranch)
		}
		if s.TargetBranch != "" {
			q.Set("searchCriteria.targetRefName", s.TargetBranch)
		}
		if s.Top > 0 {
			q.Set("$top", strconv.Itoa(s.Top))
		}
		return c.apiURL(project, apiPath, q)
	}, all)
}

// PullRequest fetches one PR by id (project-scoped lookup; keeps raw JSON).
func (c *Client) PullRequest(ctx context.Context, project string, id int) (*PullRequest, error) {
	var raw json.RawMessage
	if _, err := c.doJSON(ctx, http.MethodGet, c.apiURL(project, "git/pullrequests/"+strconv.Itoa(id), nil), nil, &raw); err != nil {
		return nil, err
	}
	var out PullRequest
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode pull request %d: %w", id, err)
	}
	out.Raw = raw
	return &out, nil
}

// CreatePullRequest opens a new pull request. body is the request payload.
func (c *Client) CreatePullRequest(ctx context.Context, project, repo string, body map[string]any) (*PullRequest, error) {
	var out PullRequest
	full := c.apiURL(project, "git/repositories/"+url.PathEscape(repo)+"/pullrequests", nil)
	if _, err := c.doJSON(ctx, http.MethodPost, full, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdatePullRequest patches a PR (title/description/status/draft/etc.).
func (c *Client) UpdatePullRequest(ctx context.Context, project, repo string, id int, body map[string]any) (*PullRequest, error) {
	var out PullRequest
	full := c.apiURL(project, "git/repositories/"+url.PathEscape(repo)+"/pullrequests/"+strconv.Itoa(id), nil)
	if _, err := c.doJSON(ctx, http.MethodPatch, full, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetReviewerVote casts a vote for a reviewer on a PR.
//
//	vote: 10 approve, 5 approve w/ suggestions, 0 reset, -5 waiting, -10 reject.
func (c *Client) SetReviewerVote(ctx context.Context, project, repo string, id int, reviewerID string, vote int) error {
	full := c.apiURL(project, "git/repositories/"+url.PathEscape(repo)+"/pullrequests/"+strconv.Itoa(id)+"/reviewers/"+url.PathEscape(reviewerID), nil)
	body := map[string]any{"vote": vote}
	_, err := c.doJSON(ctx, http.MethodPut, full, body, nil)
	return err
}

// PRThreads lists the discussion threads of a PR.
func (c *Client) PRThreads(ctx context.Context, project, repo string, id int) ([]Thread, error) {
	return getList[Thread](ctx, c, func(string) string {
		return c.apiURL(project, "git/repositories/"+url.PathEscape(repo)+"/pullrequests/"+strconv.Itoa(id)+"/threads", nil)
	}, false)
}

// AddPRComment posts a new comment thread on a PR. When tc is non-nil the
// thread is anchored inline to a file/line; otherwise it is a top-level
// (overview) comment.
func (c *Client) AddPRComment(ctx context.Context, project, repo string, id int, text string, tc *ThreadContext) (*Thread, error) {
	var out Thread
	body := map[string]any{
		"comments": []map[string]any{
			{"parentCommentId": 0, "content": text, "commentType": "text"},
		},
		"status": "active",
	}
	if tc != nil {
		body["threadContext"] = tc
	}
	full := c.apiURL(project, "git/repositories/"+url.PathEscape(repo)+"/pullrequests/"+strconv.Itoa(id)+"/threads", nil)
	if _, err := c.doJSON(ctx, http.MethodPost, full, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PRIterations lists the iterations (pushes) of a pull request, oldest first.
func (c *Client) PRIterations(ctx context.Context, project, repo string, id int) ([]PRIteration, error) {
	return getList[PRIteration](ctx, c, func(string) string {
		return c.apiURL(project, "git/repositories/"+url.PathEscape(repo)+"/pullrequests/"+strconv.Itoa(id)+"/iterations", nil)
	}, false)
}

// PRChanges lists the file changes of a PR iteration compared to compareTo.
// compareTo == 0 means the PR base, giving the cumulative diff for the whole
// pull request; compareTo == iteration-1 gives just that iteration's delta.
func (c *Client) PRChanges(ctx context.Context, project, repo string, id, iteration, compareTo int) ([]ChangeEntry, error) {
	q := url.Values{}
	q.Set("$compareTo", strconv.Itoa(compareTo))
	q.Set("$top", "2000")
	full := c.apiURL(project, "git/repositories/"+url.PathEscape(repo)+"/pullrequests/"+strconv.Itoa(id)+"/iterations/"+strconv.Itoa(iteration)+"/changes", q)
	var env struct {
		ChangeEntries []ChangeEntry `json:"changeEntries"`
	}
	if _, err := c.doJSON(ctx, http.MethodGet, full, nil, &env); err != nil {
		return nil, err
	}
	return env.ChangeEntries, nil
}

// AddPRThreadComment posts a reply to an existing comment thread. parentID is
// the comment to reply under (1 is the thread's root comment).
func (c *Client) AddPRThreadComment(ctx context.Context, project, repo string, id, threadID, parentID int, text string) (*Comment, error) {
	var out Comment
	body := map[string]any{"parentCommentId": parentID, "content": text, "commentType": "text"}
	full := c.apiURL(project, "git/repositories/"+url.PathEscape(repo)+"/pullrequests/"+strconv.Itoa(id)+"/threads/"+strconv.Itoa(threadID)+"/comments", nil)
	if _, err := c.doJSON(ctx, http.MethodPost, full, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetPRThreadStatus updates a thread's status
// (active|fixed|wontFix|closed|byDesign|pending).
func (c *Client) SetPRThreadStatus(ctx context.Context, project, repo string, id, threadID int, status string) error {
	body := map[string]any{"status": status}
	full := c.apiURL(project, "git/repositories/"+url.PathEscape(repo)+"/pullrequests/"+strconv.Itoa(id)+"/threads/"+strconv.Itoa(threadID), nil)
	_, err := c.doJSON(ctx, http.MethodPatch, full, body, nil)
	return err
}

// PRWebURL builds a browser URL for a pull request.
func (c *Client) PRWebURL(project, repo string, id int) string {
	return fmt.Sprintf("%s/%s/_git/%s/pullrequest/%d", c.orgURL, url.PathEscape(project), url.PathEscape(repo), id)
}
