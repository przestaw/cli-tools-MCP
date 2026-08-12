package client

import "encoding/json"

// IdentityRef is the common Azure DevOps user/identity reference.
type IdentityRef struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	UniqueName  string `json:"uniqueName"`
}

// ConnectionData is the subset of /_apis/connectionData we use.
type ConnectionData struct {
	AuthenticatedUser struct {
		ID                  string `json:"id"`
		ProviderDisplayName string `json:"providerDisplayName"`
		Properties          struct {
			Account struct {
				Value string `json:"$value"`
			} `json:"Account"`
		} `json:"properties"`
	} `json:"authenticatedUser"`
}

// Project is a team project.
type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	State       string `json:"state"`
	Visibility  string `json:"visibility"`
	URL         string `json:"url"`
}

// Repository is a Git repository.
type Repository struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	DefaultBranch string `json:"defaultBranch"`
	Size          int64  `json:"size"`
	WebURL        string `json:"webUrl"`
	RemoteURL     string `json:"remoteUrl"`
	SSHURL        string `json:"sshUrl"`
	IsDisabled    bool   `json:"isDisabled"`
	Project       struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"project"`

	Raw json.RawMessage `json:"-"`
}

// GitRef is a branch or tag ref.
type GitRef struct {
	Name     string `json:"name"`
	ObjectID string `json:"objectId"`
	Creator  *struct {
		DisplayName string `json:"displayName"`
	} `json:"creator,omitempty"`
}

// GitItem is a file or folder in a repository.
type GitItem struct {
	Path          string `json:"path"`
	GitObjectType string `json:"gitObjectType"`
	IsFolder      bool   `json:"isFolder"`
	ObjectID      string `json:"objectId"`
	CommitID      string `json:"commitId"`
}

// Reviewer is a PR reviewer with their vote.
//
//	vote: 10 approved, 5 approved with suggestions, 0 no vote,
//	     -5 waiting for author, -10 rejected.
type Reviewer struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	UniqueName  string `json:"uniqueName"`
	Vote        int    `json:"vote"`
	IsRequired  bool   `json:"isRequired"`
}

// PullRequest is a Git pull request.
type PullRequest struct {
	PullRequestID int         `json:"pullRequestId"`
	Title         string      `json:"title"`
	Description   string      `json:"description"`
	Status        string      `json:"status"`
	IsDraft       bool        `json:"isDraft"`
	MergeStatus   string      `json:"mergeStatus"`
	SourceRefName string      `json:"sourceRefName"`
	TargetRefName string      `json:"targetRefName"`
	CreationDate  string      `json:"creationDate"`
	ClosedDate    string      `json:"closedDate"`
	CreatedBy     IdentityRef `json:"createdBy"`
	Reviewers     []Reviewer  `json:"reviewers"`
	Repository    struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"repository"`
	LastMergeSourceCommit *struct {
		CommitID string `json:"commitId"`
	} `json:"lastMergeSourceCommit"`
	URL string `json:"url"`

	Raw json.RawMessage `json:"-"`
}

// Comment is one comment in a PR thread.
type Comment struct {
	ID            int         `json:"id"`
	ParentID      int         `json:"parentCommentId"`
	Content       string      `json:"content"`
	CommentType   string      `json:"commentType"`
	Author        IdentityRef `json:"author"`
	PublishedDate string      `json:"publishedDate"`
}

// Thread is a PR discussion thread.
type Thread struct {
	ID            int       `json:"id"`
	Status        string    `json:"status"`
	Comments      []Comment `json:"comments"`
	IsDeleted     bool      `json:"isDeleted"`
	PublishedDate string    `json:"publishedDate"`
}

// ThreadPosition is a 1-based line/column position inside a file, used to
// anchor an inline comment thread.
type ThreadPosition struct {
	Line   int `json:"line"`
	Offset int `json:"offset"`
}

// ThreadContext anchors a comment thread to a file, and optionally a line
// range within it. Right-side positions refer to the PR's new content; left-
// side positions refer to the base (deleted/original) content. A thread with
// only FilePath set is a file-level comment; add a start/end pair to pin it to
// specific lines.
type ThreadContext struct {
	FilePath       string          `json:"filePath"`
	RightFileStart *ThreadPosition `json:"rightFileStart,omitempty"`
	RightFileEnd   *ThreadPosition `json:"rightFileEnd,omitempty"`
	LeftFileStart  *ThreadPosition `json:"leftFileStart,omitempty"`
	LeftFileEnd    *ThreadPosition `json:"leftFileEnd,omitempty"`
}

// PRIteration is one iteration (push/update) of a pull request.
type PRIteration struct {
	ID          int         `json:"id"`
	Description string      `json:"description"`
	CreatedDate string      `json:"createdDate"`
	Author      IdentityRef `json:"author"`
	// SourceRefCommit is the source-branch tip for this iteration; CommonRefCommit
	// is the merge base with the target. Diffing common..source gives the whole PR.
	SourceRefCommit CommitRef `json:"sourceRefCommit"`
	TargetRefCommit CommitRef `json:"targetRefCommit"`
	CommonRefCommit CommitRef `json:"commonRefCommit"`
}

// CommitRef references a Git commit by id.
type CommitRef struct {
	CommitID string `json:"commitId"`
}

// ChangeEntry is one changed file/folder in a PR iteration.
type ChangeEntry struct {
	ChangeType string `json:"changeType"`
	Item       struct {
		Path          string `json:"path"`
		IsFolder      bool   `json:"isFolder"`
		GitObjectType string `json:"gitObjectType"`
	} `json:"item"`
	OriginalPath string `json:"originalPath"`
}

// IdentityMatch is a user/group returned by the IdentityPicker (typeahead)
// search. LocalId is the identity id used by PR reviewers and votes.
type IdentityMatch struct {
	LocalID       string `json:"localId"`
	DisplayName   string `json:"displayName"`
	SignInAddress string `json:"signInAddress"`
	Mail          string `json:"mail"`
	EntityType    string `json:"entityType"`
}

// Account returns the sign-in address (or mail) of a matched identity.
func (i IdentityMatch) Account() string {
	if i.SignInAddress != "" {
		return i.SignInAddress
	}
	return i.Mail
}

// Pipeline is a YAML/Classic pipeline (Pipelines API).
type Pipeline struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Folder   string `json:"folder"`
	Revision int    `json:"revision"`
	URL      string `json:"url"`
}

// Run is one run of a pipeline (Pipelines API).
type Run struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	State        string `json:"state"`
	Result       string `json:"result"`
	CreatedDate  string `json:"createdDate"`
	FinishedDate string `json:"finishedDate"`
	URL          string `json:"url"`
	Pipeline     struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"pipeline"`

	Raw json.RawMessage `json:"-"`
}

// BuildDefinition is a classic/YAML build definition (Build API).
type BuildDefinition struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	Revision    int    `json:"revision"`
	QueueStatus string `json:"queueStatus"`
	Type        string `json:"type"`
}

// Build is one build (Build API).
type Build struct {
	ID           int    `json:"id"`
	BuildNumber  string `json:"buildNumber"`
	Status       string `json:"status"`
	Result       string `json:"result"`
	SourceBranch string `json:"sourceBranch"`
	QueueTime    string `json:"queueTime"`
	StartTime    string `json:"startTime"`
	FinishTime   string `json:"finishTime"`
	Definition   struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"definition"`
	RequestedBy IdentityRef `json:"requestedBy"`
	URL         string      `json:"url"`

	Raw json.RawMessage `json:"-"`
}

// BuildLog is one log of a build.
type BuildLog struct {
	ID        int    `json:"id"`
	Type      string `json:"type"`
	LineCount int    `json:"lineCount"`
	URL       string `json:"url"`
}

// ReleaseDefinition is a classic release pipeline (Release Management API).
type ReleaseDefinition struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Source string `json:"source"`
}

// Release is one release (Release Management API).
type Release struct {
	ID                int         `json:"id"`
	Name              string      `json:"name"`
	Status            string      `json:"status"`
	CreatedOn         string      `json:"createdOn"`
	CreatedBy         IdentityRef `json:"createdBy"`
	ReleaseDefinition struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"releaseDefinition"`
	Environments []ReleaseEnvironment `json:"environments"`
	URL          string               `json:"url"`

	Raw json.RawMessage `json:"-"`
}

// ReleaseEnvironment is one stage of a release.
type ReleaseEnvironment struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}
