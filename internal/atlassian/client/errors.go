package client

import "workctl/internal/httpx"

// APIError represents a non-2xx response from an Atlassian API.
type APIError struct {
	Status  int    // HTTP status code
	Message string // parsed error message (or raw body)
	URL     string // request URL for context
}

func (e *APIError) Error() string {
	return httpx.ErrorMessage("atlassian", e.Status, e.Message, e.URL)
}

// IsNotFound reports whether err is a 404 APIError.
func IsNotFound(err error) bool {
	if e, ok := err.(*APIError); ok {
		return e.Status == 404
	}
	return false
}
