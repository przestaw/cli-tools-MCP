package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

func cmdGitLab(ctx context.Context, client *APIClient, args []string) error {
	var path string
	switch {
	case len(args) == 3 && args[0] == "project" && args[1] == "get":
		path = "/api/v4/projects/" + url.PathEscape(args[2])
	case len(args) == 4 && args[0] == "mr" && args[1] == "get":
		if _, err := strconv.Atoi(args[3]); err != nil {
			return fmt.Errorf("merge request ID must be a number")
		}
		path = "/api/v4/projects/" + url.PathEscape(args[2]) + "/merge_requests/" + args[3]
	case len(args) >= 3 && args[0] == "raw":
		body, err := client.do(ctx, args[1], args[2], nil)
		if err != nil {
			return err
		}
		return printBody(body)
	default:
		return fmt.Errorf("usage: workctl gitlab project get GROUP/PROJECT | gitlab mr get GROUP/PROJECT ID | gitlab raw METHOD PATH")
	}
	body, err := client.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	return printBody(body)
}
