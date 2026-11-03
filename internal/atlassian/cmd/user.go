// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"github.com/spf13/cobra"
	"workctl/internal/atlassian/client"
)

var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Jira users",
}

var userMeCmd = &cobra.Command{
	Use:   "me",
	Short: "The authenticated user",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		u, err := svc.Client.Myself(c)
		if err != nil {
			return err
		}
		return printUser(u)
	},
}

var userGetCmd = &cobra.Command{
	Use:   "get <accountId>",
	Short: "Get a user by accountId",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		u, err := svc.Client.UserByAccountID(c, args[0])
		if err != nil {
			return err
		}
		return printUser(u)
	},
}

var userFindCmd = &cobra.Command{
	Use:   "find <query>",
	Short: "Search users by name or email",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		us, err := svc.Client.FindUsers(c, args[0])
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(us)
		}
		rows := make([][]string, len(us))
		for i, u := range us {
			active := ""
			if !u.Active {
				active = "inactive"
			}
			rows[i] = []string{u.AccountID, u.DisplayName, u.EmailAddress, active}
		}
		return p.Table([]string{"AccountId", "Name", "Email", "State"}, rows)
	},
}

func printUser(u *client.User) error {
	p := printer()
	if p.JSON {
		return p.JSONValue(u)
	}
	return p.Table([]string{"Field", "Value"}, [][]string{
		{"AccountId", u.AccountID},
		{"Name", u.DisplayName},
		{"Email", u.EmailAddress},
	})
}

func init() {
	userCmd.AddCommand(userMeCmd, userGetCmd, userFindCmd)
	rootCmd.AddCommand(userCmd)
}
