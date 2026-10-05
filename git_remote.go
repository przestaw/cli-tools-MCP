package main

import (
	"fmt"
	"net/url"
	"strings"
)

// GitRemote identifies the server and repository path in an HTTPS or SSH Git remote.
type GitRemote struct {
	Host string
	Path string
}

func parseGitRemote(raw string) (GitRemote, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return GitRemote{}, fmt.Errorf("Git remote is empty")
	}
	if at := strings.Index(raw, "@"); at > 0 && !strings.Contains(raw[:at], "://") {
		hostPath := raw[at+1:]
		parts := strings.SplitN(hostPath, ":", 2)
		if len(parts) != 2 {
			return GitRemote{}, fmt.Errorf("invalid SSH Git remote %q", raw)
		}
		return normalizedGitRemote(parts[0], parts[1])
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "ssh") {
		return GitRemote{}, fmt.Errorf("unsupported Git remote %q", raw)
	}
	return normalizedGitRemote(parsed.Hostname(), parsed.Path)
}

func normalizedGitRemote(host, path string) (GitRemote, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	path = strings.TrimSuffix(strings.Trim(strings.TrimSpace(path), "/"), ".git")
	if host == "" || path == "" || strings.Contains(path, "..") {
		return GitRemote{}, fmt.Errorf("invalid Git remote")
	}
	return GitRemote{Host: host, Path: path}, nil
}
