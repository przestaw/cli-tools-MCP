# workctl

Author: [przestaw](https://github.com/przestaw)

`workctl` is a small, provider-neutral command-line foundation for developer-service APIs. It keeps shared options,
configuration, HTTP behavior and output handling in one binary so provider modules can be added without duplicating
transport code.

The first version exposes guarded raw API access and reserves stable command names for planned modules.

Azure DevOps is available as `workctl azure ...` (short alias: `workctl ado ...`), including repositories, pull
requests, pipelines, builds and releases. Run `workctl azure config init` to create a profile and store its token in
the operating system's credential store.

Atlassian Cloud is available as `workctl atlassian ...` (short alias: `workctl atl ...`). The lightweight Jira module
uses the common endpoint configuration and offers `workctl jira issue get DEMO-123` plus guarded raw requests.

GitLab is available as `workctl gitlab ...`, with project and merge-request lookups plus guarded raw requests. Project
paths are supplied on the command line or in neutral local configuration; the repository contains no organization or
project defaults.

```powershell
workctl --url https://service.example --token-env SERVICE_TOKEN raw GET /api/status
workctl --config config.json raw POST /api/items --data '{"name":"demo"}'
workctl bitbucket
```

Global options:

- `--config FILE`: load JSON configuration;
- `--url URL`: override the configured API base URL;
- `--token-env NAME`: read a bearer token from an environment variable;
- `--timeout SECONDS`: set the request timeout;
- `--insecure`: explicitly disable TLS verification.

Absolute URLs are rejected by `raw`, redirects to another host are blocked, and response bodies are formatted as JSON
when possible. Tests that exercise the public CLI live under `tests/`.

Planned modules are listed in `planned.go`. Provider-specific modules will keep the same global option conventions.

## License

[Creative Commons Attribution 4.0 International (CC BY 4.0)](LICENSE) - [przestaw](https://github.com/przestaw)
