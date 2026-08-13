package client

import "fmt"

// APIError represents a non-2xx response from an Atlassian API.
type APIError struct {
	Status  int    // HTTP status code
	Message string // parsed error message (or raw body)
	URL     string // request URL for context
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("atlassian %d for %s", e.Status, e.URL)
	}
	return fmt.Sprintf("atlassian %d: %s", e.Status, e.Message)
}

// IsNotFound reports whether err is a 404 APIError.
func IsNotFound(err error) bool {
	if e, ok := err.(*APIError); ok {
		return e.Status == 404
	}
	return false
}
