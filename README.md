# workctl

Author: [przestaw](https://github.com/przestaw)

`workctl` is a small, provider-neutral command-line foundation for developer-service APIs. It keeps shared options,
configuration, HTTP behavior and output handling in one binary so provider modules can be added without duplicating
transport code.

The first version exposes guarded raw API access and reserves stable command names for planned modules.

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
