// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package output

import (
	"fmt"
	"strconv"

	"workctl/internal/atlassian/client"
)

// Page prints a Confluence page as a FIELD / VALUE table. With Expand on, the
// storage body is rendered as a text block below the table.
func (p *Printer) Page(pg *client.Page, wikiURL string) error {
	var rows [][]string
	add := func(k, v string) {
		if v == "" {
			return
		}
		rows = append(rows, []string{k, v})
	}
	add("ID", pg.ID)
	add("Title", pg.Title)
	add("SpaceID", pg.SpaceID)
	add("ParentID", pg.ParentID)
	add("Status", pg.Status)
	if pg.Version != nil {
		add("Version", strconv.Itoa(pg.Version.Number))
		add("Modified", pg.Version.CreatedAt)
	}
	body := ""
	if pg.Body != nil && pg.Body.Storage != nil {
		body = p.StorageToText(pg.Body.Storage.Value)
	}
	if p.Expand && body != "" {
		add("Body", "(see below)")
	} else {
		add("Body", body)
	}
	if err := p.Table([]string{"Field", "Value"}, rows); err != nil {
		return err
	}
	if p.Expand && body != "" {
		fmt.Fprintf(p.out(), "\n%s\n%s\n", p.bold("Body:"), body)
	}
	if wikiURL != "" && !p.JSON {
		fmt.Fprintf(p.out(), "\n%s\n", p.dim(wikiURL))
	}
	return nil
}

// PageTable renders the standard page list table.
func (p *Printer) PageTable(pages []client.Page) error {
	rows := make([][]string, len(pages))
	for i, pg := range pages {
		version := ""
		if pg.Version != nil {
			version = strconv.Itoa(pg.Version.Number)
		}
		rows[i] = []string{pg.ID, pg.Title, pg.Status, version, pg.ParentID}
	}
	return p.Table([]string{"ID", "Title", "Status", "Version", "ParentID"}, rows)
}
