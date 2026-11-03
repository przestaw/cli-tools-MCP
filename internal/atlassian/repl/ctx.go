// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package repl

// level identifies where the user currently is in the navigation tree.
// Jira side: /jira > project > issue. Confluence side: /wiki > space > page.
type level int

const (
	levelRoot level = iota
	levelJira
	levelProject
	levelIssue
	levelWiki
	levelSpace
	levelPage
)

// navCtx tracks the current navigation context of the REPL.
type navCtx struct {
	level      level
	projectKey string
	issueKey   string
	spaceKey   string
	spaceID    string
	pageID     string
	isFolder   bool // pageID refers to a folder, not a page
}

// reset returns to root.
func (n *navCtx) reset() { *n = navCtx{} }

// up pops one level toward root.
func (n *navCtx) up() {
	switch n.level {
	case levelIssue:
		n.issueKey = ""
		n.level = levelProject
	case levelProject:
		n.projectKey = ""
		n.level = levelJira
	case levelJira:
		n.level = levelRoot
	case levelPage:
		n.pageID = ""
		n.isFolder = false
		n.level = levelSpace
	case levelSpace:
		n.spaceKey, n.spaceID = "", ""
		n.level = levelWiki
	case levelWiki:
		n.level = levelRoot
	}
}

// path renders the context as a full path (pwd).
func (n *navCtx) path() string {
	switch n.level {
	case levelJira:
		return "/jira"
	case levelProject:
		return "/jira/" + n.projectKey
	case levelIssue:
		return "/jira/" + n.projectKey + "/" + n.issueKey
	case levelWiki:
		return "/wiki"
	case levelSpace:
		return "/wiki/" + n.spaceKey
	case levelPage:
		return "/wiki/" + n.spaceKey + "/" + n.pageID
	default:
		return "/"
	}
}
