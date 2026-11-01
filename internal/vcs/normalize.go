package vcs

import (
	"encoding/json"
	"fmt"
)

// Fill extracts the provider-specific fields of one pull request into the shared PullRequest.
type Fill func(*PullRequest)

// Normalize turns a provider response (an array, a single object, or a search result with "items")
// into the shared Result. fill maps the provider fields; it must set at least the ID.
func Normalize(provider, repository string, body []byte, fill Fill) (Result, error) {
	var values []map[string]any
	if err := json.Unmarshal(body, &values); err != nil {
		var one map[string]any
		if err := json.Unmarshal(body, &one); err != nil {
			return Result{}, err
		}
		if items, ok := one["items"].([]any); ok {
			for _, item := range items {
				if value, ok := item.(map[string]any); ok {
					values = append(values, value)
				}
			}
		} else {
			values = []map[string]any{one}
		}
	}
	result := Result{Items: make([]PullRequest, 0, len(values))}
	for _, value := range values {
		pr := PullRequest{Provider: provider, Repository: repository, Details: value}
		pr.Title, _ = value["title"].(string)
		pr.Draft, _ = value["draft"].(bool)
		fill(&pr)
		if pr.ID <= 0 {
			return Result{}, fmt.Errorf("%s response does not contain a pull request number", provider)
		}
		result.Items = append(result.Items, pr)
	}
	return result, nil
}

// Single adapts a one-pull-request response: it normalizes body and returns the first item,
// or the notFound message when the response holds none.
func Single(provider, repository, notFound string, body []byte, requestErr error, fill Fill) (PullRequest, error) {
	if requestErr != nil {
		return PullRequest{}, requestErr
	}
	res, err := Normalize(provider, repository, body, fill)
	if err != nil {
		return PullRequest{}, err
	}
	if len(res.Items) == 0 {
		return PullRequest{}, fmt.Errorf("%s", notFound)
	}
	return res.Items[0], nil
}

// Number reads a JSON number as a non-negative integer id; anything else yields 0.
func Number(value any) int {
	number, ok := value.(float64)
	if !ok || number != float64(int(number)) {
		return 0
	}
	return int(number)
}
