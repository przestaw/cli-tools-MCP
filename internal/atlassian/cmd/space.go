package cmd

import (
	"github.com/spf13/cobra"
)

var spaceCmd = &cobra.Command{
	Use:   "space",
	Short: "Confluence spaces",
}

var spaceListCmd = &cobra.Command{
	Use:   "list [key ...]",
	Short: "List spaces (optionally filtered by key)",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		spaces, err := svc.Client.SpacesAll(c, args)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(spaces)
		}
		rows := make([][]string, len(spaces))
		for i, s := range spaces {
			rows[i] = []string{s.Key, s.Name, s.ID, s.Type, s.Status}
		}
		return p.Table([]string{"Key", "Name", "ID", "Type", "Status"}, rows)
	},
}

var spaceGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a space (full JSON; key or numeric id)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		id := args[0]
		if !isDigits(id) {
			sp, err := svc.Client.SpaceByKey(c, id)
			if err != nil {
				return err
			}
			id = sp.ID
		}
		raw, err := svc.Client.SpaceRaw(c, id)
		if err != nil {
			return err
		}
		return printer().Raw(raw)
	},
}

var spacePagesCmd = &cobra.Command{
	Use:   "pages <key>",
	Short: "List a space's pages (most recently modified first)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		id := args[0]
		if !isDigits(id) {
			sp, err := svc.Client.SpaceByKey(c, id)
			if err != nil {
				return err
			}
			id = sp.ID
		}
		pages, cursor, err := svc.Client.SpacePages(c, id, "", flagPageSize)
		if err != nil {
			return err
		}
		if flagAll {
			for cursor != "" {
				more, next, err := svc.Client.SpacePages(c, id, cursor, flagPageSize)
				if err != nil {
					return err
				}
				pages = append(pages, more...)
				if next == "" || len(more) == 0 {
					break
				}
				cursor = next
			}
		}
		if p.JSON {
			return p.JSONValue(pages)
		}
		if err := p.PageTable(pages); err != nil {
			return err
		}
		if !flagQuiet && cursor != "" && !flagAll {
			p.Msg("more pages available (use --all)")
		}
		return nil
	},
}

// isDigits reports whether s is a non-empty numeric string.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func init() {
	addPagingFlags(spacePagesCmd, 50)
	spaceCmd.AddCommand(spaceListCmd, spaceGetCmd, spacePagesCmd)
	rootCmd.AddCommand(spaceCmd)
}
