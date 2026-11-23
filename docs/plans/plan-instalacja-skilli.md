# Plan: instalacja i aktualizacja skilli agentów

Status: PR #17 (gałąź feat/install-skills-pr), do przeglądu. Autor: [przestaw](https://github.com/przestaw)

## Problemy

1. Kopie skilla (`~/.copilot/skills`, `~/.claude/skills`, `$CODEX_HOME/skills`) po cichu się starzeją; nic tego nie zgłasza.
   Na jednej maszynie wszystkie trzy kopie różniły się od repozytorium.
2. Drugi skill (`compact-output`) nie był instalowany żadną ścieżką.
3. Zarejestrowany już serwer MCP `workctl` był pozostawiany, nawet gdy wskazuje inny plik binarny.
4. Plugin Claude Code (marketplace) i kopia skilla z instalatora dają dwa skille o nazwie `workctl`.

## Decyzje

- Skille są wbudowane w binarkę (`plugins/embed.go`, `go:embed`); jedno źródło prawdy to `plugins/workctl/skills`,
  nowy katalog skilla nie wymaga innych zmian (pilnuje tego test).
- Pakiet `internal/skills`: cele (vscode, claude, codex), stan `missing|current|differs` (porównanie SHA-256 plików),
  instalacja tylko plików wbudowanych skilli, bez usuwania czegokolwiek, zapis atomowy (plik tymczasowy + rename), idempotentna.
  Domyślnie `workctl`; `compact-output` tylko z `--with`.
- Polecenie CLI `workctl skills status|install [--target ...] [--with compact-output] [--dry-run] [--check]`;
  tylko CLI, nie narzędzie MCP.
- `workctl doctor`: wpis `skills`; przestarzała kopia daje `ok=false`, kod `outdated` i polecenie naprawy.
  Cele bez zainstalowanej kopii nie są błędem. Wpis jest informacyjny (jak moduły azure/atlassian): nie zmienia kodu wyjścia,
  który zależy tylko od profili.
- Duplikat: `skills status` ostrzega, gdy `~/.claude/plugins/installed_plugins.json` zawiera `workctl@...` i istnieje
  `~/.claude/skills/workctl` (układ sprawdzony tylko do odczytu na tej maszynie).
- `scripts/install.py` nie kopiuje już plików: woła `workctl skills install --target X`; dodano `--with-compact-output`
  i `--skills-only`; dla już zarejestrowanego serwera ostrzega, gdy jego polecenie to inny plik binarny (konfiguracji nie zmienia).

## Świadomie nie zrobiono

- Gotowe binarki w wydaniach i podpisywanie: osobna decyzja.
- Automatyczna zmiana cudzej konfiguracji MCP ani usuwanie skilli/pluginów.
- Poprawka zduplikowanej sekcji Google w `SKILL.md` (osobna zmiana).
