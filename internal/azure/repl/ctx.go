// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package repl

// level identifies where the user currently is in the navigation tree:
// root > project > repo.
type level int

const (
	levelRoot level = iota
	levelProject
	levelRepo
)

// navCtx tracks the current navigation context of the REPL.
type navCtx struct {
	level   level
	project string
	repo    string
}

// reset returns to root.
func (n *navCtx) reset() { *n = navCtx{} }

// up pops one level toward root.
func (n *navCtx) up() {
	switch n.level {
	case levelRepo:
		n.repo = ""
		n.level = levelProject
	case levelProject:
		n.project = ""
		n.level = levelRoot
	}
}

// path renders the context as a full path (pwd).
func (n *navCtx) path() string {
	switch n.level {
	case levelProject:
		return "/" + n.project
	case levelRepo:
		return "/" + n.project + "/" + n.repo
	default:
		return "/"
	}
}
