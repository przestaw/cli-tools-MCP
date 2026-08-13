package client

import "encoding/json"

// --- Jira core types ---

// Named is any Jira object carrying id + name (status, priority, issue type...).
type Named struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// User is a Jira Cloud user (identified by accountId, not username).
type User struct {
	AccountID    string `json:"accountId"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
	Active       bool   `json:"active"`
}

// Project is a Jira project.
type Project struct {
	ID         string `json:"id"`
	Key        string `json:"key"`
	Name       string `json:"name"`
	TypeKey    string `json:"projectTypeKey"`
	Style      string `json:"style"` // classic | next-gen (team-managed)
	IsPrivate  bool   `json:"isPrivate"`
	Archived   bool   `json:"archived"`
	Lead       *User  `json:"lead"`
	Simplified bool   `json:"simplified"`
}

// ProjectPage is the paged response of /rest/api/3/project/search.
type ProjectPage struct {
	StartAt    int       `json:"startAt"`
	MaxResults int       `json:"maxResults"`
	Total      int       `json:"total"`
	IsLast     bool      `json:"isLast"`
	Values     []Project `json:"values"`
}

// IssueRef is a minimal issue reference (parent, subtask, link target).
type IssueRef struct {
	ID     string          `json:"id"`
	Key    string          `json:"key"`
	Fields *IssueRefFields `json:"fields"`
}

// IssueRefFields are the fields embedded in an issue reference.
type IssueRefFields struct {
	Summary   string `json:"summary"`
	Status    *Named `json:"status"`
	IssueType *Named `json:"issuetype"`
	Priority  *Named `json:"priority"`
}

// Issue is a Jira issue with the commonly used fields typed and everything
// (including custom fields) retained raw in Fields.
type Issue struct {
	ID        string            `json:"id"`
	Key       string            `json:"key"`
	Self      string            `json:"self"`
	Fields    IssueFields       `json:"fields"`
	Names     map[string]string `json:"names"`     // field id -> display name (expand=names)
	Raw       json.RawMessage   `json:"-"`         // full original body
	Changelog *Changelog        `json:"changelog"` // expand=changelog
}

// IssueFields are the typed well-known issue fields. Custom holds every other
// (custom) field raw, keyed by field id.
type IssueFields struct {
	Summary     string          `json:"summary"`
	Description json.RawMessage `json:"description"` // ADF
	Status      *Named          `json:"status"`
	IssueType   *Named          `json:"issuetype"`
	Priority    *Named          `json:"priority"`
	Resolution  *Named          `json:"resolution"`
	Assignee    *User           `json:"assignee"`
	Reporter    *User           `json:"reporter"`
	Creator     *User           `json:"creator"`
	Labels      []string        `json:"labels"`
	Components  []Named         `json:"components"`
	FixVersions []Named         `json:"fixVersions"`
	Project     *Project        `json:"project"`
	Parent      *IssueRef       `json:"parent"`
	Subtasks    []IssueRef      `json:"subtasks"`
	IssueLinks  []IssueLink     `json:"issuelinks"`
	Created     string          `json:"created"`
	Updated     string          `json:"updated"`
	DueDate     string          `json:"duedate"`
	Attachment  []Attachment    `json:"attachment"`

	Custom map[string]json.RawMessage `json:"-"` // customfield_* values
}

// issueFieldsAlias avoids recursion in UnmarshalJSON.
type issueFieldsAlias IssueFields

// UnmarshalJSON decodes the known fields and collects customfield_* extras.
func (f *IssueFields) UnmarshalJSON(data []byte) error {
	var a issueFieldsAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*f = IssueFields(a)
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	f.Custom = map[string]json.RawMessage{}
	for k, v := range all {
		if len(k) > 12 && k[:12] == "customfield_" {
			f.Custom[k] = v
		}
	}
	return nil
}

// IssueLink is one entry of an issue's issuelinks field. Exactly one of
// InwardIssue/OutwardIssue is set, defining the link direction.
type IssueLink struct {
	ID           string    `json:"id"`
	Type         LinkType  `json:"type"`
	InwardIssue  *IssueRef `json:"inwardIssue"`
	OutwardIssue *IssueRef `json:"outwardIssue"`
}

// LinkType is a Jira issue link type with its directional phrases.
type LinkType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Inward  string `json:"inward"`
	Outward string `json:"outward"`
}

// SearchPage is the response of /rest/api/3/search/jql (token pagination).
type SearchPage struct {
	Issues        []Issue           `json:"issues"`
	NextPageToken string            `json:"nextPageToken"`
	IsLast        bool              `json:"isLast"`
	Names         map[string]string `json:"names"`
}

// Transition is one workflow transition available on an issue.
type Transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	To   *Named `json:"to"`
}

// Comment is a Jira issue comment (body is ADF).
type Comment struct {
	ID      string          `json:"id"`
	Author  *User           `json:"author"`
	Body    json.RawMessage `json:"body"`
	Created string          `json:"created"`
	Updated string          `json:"updated"`
}

// CommentPage is the paged comment list response.
type CommentPage struct {
	StartAt    int       `json:"startAt"`
	MaxResults int       `json:"maxResults"`
	Total      int       `json:"total"`
	Comments   []Comment `json:"comments"`
}

// Attachment is a Jira issue attachment.
type Attachment struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Author   *User  `json:"author"`
	Created  string `json:"created"`
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
	Content  string `json:"content"` // download URL
}

// UnmarshalJSON accepts `id` as either a JSON string or a number. Jira is
// inconsistent: an issue's `fields.attachment[]` carries `"id": "63790"` while
// GET /rest/api/3/attachment/{id} returns `"id": 63790`.
func (a *Attachment) UnmarshalJSON(data []byte) error {
	type plain Attachment // strip this method, so the nested decode won't recurse
	var raw struct {
		plain
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*a = Attachment(raw.plain)
	switch {
	case len(raw.ID) == 0 || string(raw.ID) == "null":
		a.ID = ""
	case raw.ID[0] == '"':
		return json.Unmarshal(raw.ID, &a.ID)
	default:
		a.ID = string(raw.ID)
	}
	return nil
}

// Changelog is the issue change history (expand=changelog).
type Changelog struct {
	Total     int                `json:"total"`
	Histories []ChangelogHistory `json:"histories"`
}

// ChangelogHistory is one change set (one save) in the changelog.
type ChangelogHistory struct {
	ID      string          `json:"id"`
	Author  *User           `json:"author"`
	Created string          `json:"created"`
	Items   []ChangelogItem `json:"items"`
}

// ChangelogItem is one field change within a change set.
type ChangelogItem struct {
	Field      string `json:"field"`
	FromString string `json:"fromString"`
	ToString   string `json:"toString"`
}

// Filter is a saved Jira filter.
type Filter struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	JQL   string `json:"jql"`
	Owner *User  `json:"owner"`
}

// FilterPage is the paged response of /rest/api/3/filter/search.
type FilterPage struct {
	StartAt    int      `json:"startAt"`
	MaxResults int      `json:"maxResults"`
	Total      int      `json:"total"`
	IsLast     bool     `json:"isLast"`
	Values     []Filter `json:"values"`
}

// Field is a Jira field definition from /rest/api/3/field.
type Field struct {
	ID     string       `json:"id"`
	Key    string       `json:"key"`
	Name   string       `json:"name"`
	Custom bool         `json:"custom"`
	Schema *FieldSchema `json:"schema"`
}

// FieldSchema describes a field's value type.
type FieldSchema struct {
	Type   string `json:"type"`   // string, number, array, option, user, ...
	Items  string `json:"items"`  // for arrays: element type
	Custom string `json:"custom"` // custom field kind URI
	System string `json:"system"` // system field name
}

// FieldMeta is a field entry of createmeta/editmeta: schema + allowed values.
type FieldMeta struct {
	FieldID         string          `json:"fieldId"` // createmeta
	Key             string          `json:"key"`     // editmeta
	Name            string          `json:"name"`
	Required        bool            `json:"required"`
	Schema          *FieldSchema    `json:"schema"`
	AllowedValues   json.RawMessage `json:"allowedValues"`
	HasDefaultValue bool            `json:"hasDefaultValue"`
	Operations      []string        `json:"operations"`
}

// ID returns the field id regardless of createmeta/editmeta shape.
func (m FieldMeta) IDKey() string {
	if m.FieldID != "" {
		return m.FieldID
	}
	return m.Key
}

// IssueType is a Jira issue type (createmeta shape).
type IssueType struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Subtask     bool   `json:"subtask"`
	Description string `json:"description"`
}

// --- Agile (boards / sprints) ---

// Board is a Jira software board.
type Board struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"` // scrum | kanban
	Location struct {
		ProjectKey  string `json:"projectKey"`
		ProjectName string `json:"projectName"`
	} `json:"location"`
}

// Sprint is a Jira scrum sprint.
type Sprint struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"` // future | active | closed
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Goal      string `json:"goal"`
}

// AgilePage is the generic agile API page envelope.
type AgilePage[T any] struct {
	StartAt    int  `json:"startAt"`
	MaxResults int  `json:"maxResults"`
	Total      int  `json:"total"`
	IsLast     bool `json:"isLast"`
	Values     []T  `json:"values"`
}

// --- Confluence types ---

// Space is a Confluence space (v2 API).
type Space struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Name   string `json:"name"`
	Type   string `json:"type"`   // global | personal
	Status string `json:"status"` // current | archived
	Links  struct {
		WebUI string `json:"webui"`
	} `json:"_links"`
}

// Page is a Confluence page (v2 API).
type Page struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Title    string `json:"title"`
	SpaceID  string `json:"spaceId"`
	ParentID string `json:"parentId"`
	Version  *struct {
		Number    int    `json:"number"`
		CreatedAt string `json:"createdAt"`
	} `json:"version"`
	Body *struct {
		Storage *struct {
			Value          string `json:"value"`
			Representation string `json:"representation"`
		} `json:"storage"`
	} `json:"body"`
	Links struct {
		WebUI string `json:"webui"`
	} `json:"_links"`
}

// V2Page is the cursor-paged envelope of Confluence v2 list endpoints.
type V2Page[T any] struct {
	Results []T `json:"results"`
	Links   struct {
		Next string `json:"next"`
	} `json:"_links"`
}

// PageAttachment is a Confluence page attachment (v2 API).
type PageAttachment struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Title        string `json:"title"`
	MediaType    string `json:"mediaType"`
	FileSize     int64  `json:"fileSize"`
	PageID       string `json:"pageId"`
	DownloadLink string `json:"downloadLink"`
}

// ContentChild is one typed entry of a v2 direct-children listing
// (pages, folders, whiteboards, databases...).
type ContentChild struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Type          string `json:"type"` // page | folder | whiteboard | database | embed
	Status        string `json:"status"`
	ChildPosition int64  `json:"childPosition"`
}

// FooterComment is a Confluence page footer comment (v2 API).
type FooterComment struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Title   string `json:"title"`
	Version *struct {
		Number    int    `json:"number"`
		CreatedAt string `json:"createdAt"`
		AuthorID  string `json:"authorId"`
	} `json:"version"`
	Body *struct {
		Storage *struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
}

// CQLResult is one hit of the v1 content search (CQL).
type CQLResult struct {
	ID    string `json:"id"`
	Type  string `json:"type"` // page | blogpost | attachment | comment
	Title string `json:"title"`
	Space *struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"space"`
	Links struct {
		WebUI string `json:"webui"`
	} `json:"_links"`
}

// CQLPage is the v1 content search page envelope.
type CQLPage struct {
	Results   []CQLResult `json:"results"`
	Start     int         `json:"start"`
	Limit     int         `json:"limit"`
	Size      int         `json:"size"`
	TotalSize int         `json:"totalSize"`
}
