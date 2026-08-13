package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

func cmdJira(ctx context.Context, client *APIClient, args []string) error {
	if len(args) == 3 && args[0] == "issue" && args[1] == "get" {
		body, err := client.do(ctx, http.MethodGet, "/rest/api/2/issue/"+url.PathEscape(args[2]), nil)
		if err != nil {
			return err
		}
		return printBody(body)
	}
	if len(args) >= 3 && args[0] == "raw" {
		body, err := client.do(ctx, args[1], args[2], nil)
		if err != nil {
			return err
		}
		return printBody(body)
	}
	return fmt.Errorf("usage: workctl jira issue get ISSUE-KEY | jira raw METHOD PATH")
}
