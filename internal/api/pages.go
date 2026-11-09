// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// pageSize is the largest page GitHub and GitLab serve.
const pageSize = 100

// GetAllPages reads a JSON array page by page (per_page=100, page=1, 2, ...) until a page comes back short, and
// returns the items as one array. An answer that is not an array is returned as it is.
// shortcut: stops after maxPages pages and counts items instead of reading Link headers; raise maxPages if a list
// ever needs more (GitHub itself returns at most 3000 files of a pull request).
func (c *Client) GetAllPages(ctx context.Context, path string, q url.Values, maxPages int) ([]byte, error) {
	all := []json.RawMessage{}
	for page := 1; page <= maxPages; page++ {
		pq := url.Values{}
		for k, v := range q {
			pq[k] = v
		}
		pq.Set("per_page", strconv.Itoa(pageSize))
		pq.Set("page", strconv.Itoa(page))
		body, err := BodyOnly(c.Do(ctx, http.MethodGet, path, pq, nil, ""))
		if err != nil {
			return nil, err
		}
		var items []json.RawMessage
		if err := json.Unmarshal(body, &items); err != nil {
			if page == 1 {
				return body, nil
			}
			return nil, err
		}
		all = append(all, items...)
		if len(items) < pageSize {
			return json.Marshal(all)
		}
	}
	return nil, fmt.Errorf("pagination limit reached (%d pages, %d items): result may be truncated", maxPages, len(all))
}
