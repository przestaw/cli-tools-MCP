# Plan: luki CLI zgłoszone przez agenta

Autor: [przestaw](https://github.com/przestaw)

Status (2026-11-17): punkty 1–9 wdrożone w `draft/simplify-core` (pkt 8 i 9 także w PR #8), plus pomoc CLI (`help`, `tools`). Źródło: raport agenta, który pracował z workctl z linii
komend i musiał improwizować. Wszystkie punkty sprawdzone w kodzie na `dev`.

Zasady: CLI i MCP korzystają z tych samych funkcji (bez drugiej implementacji), kod prosty i bez nowych
frameworków, każda zmiana to osobny commit z testem. Wersja: wszystko poniżej nie zmienia układu keyringa,
więc `Z` (0.4.x).

## Podsumowanie

| # | Luka | Typ | Rozmiar | Kolejność |
| --- | --- | --- | --- | --- |
| 2 | `api raw` po cichu ignoruje `--profile` innego rodzaju | błąd (bezpieczeństwo użycia) | mały | 1 |
| 7 | `config show` pokazuje identyfikatory OAuth i inne pola | bezpieczeństwo | mały | 2 |
| 3 | Brak `accounts` w CLI, `mr list` każe zgadywać profil | brakująca komenda | mały | 3 |
| 6 | `doctor` nie rozróżnia DNS, timeoutu i 401 | diagnostyka | mały | 4 |
| 1 | `mr show` bez `--include` | parytet CLI z MCP | średni | 5 |
| 4 | Brak CLI `download` z metadanymi | parytet CLI z MCP | mały | 6 |

## 2. `api raw` ignoruje `--profile` innego rodzaju

**Przyczyna.** `generic.Command` woła `api.NewKindClient(cfg, "api")`, czyli `cfg.ProfileOf("api")`. Ta funkcja
bierze wskazany profil tylko gdy jego rodzaj to `api`; inaczej po cichu sięga po `defaults.api` (u agenta: inna usługa).
Groźne, bo zapis idzie do innej usługi niż myśli użytkownik. Wersja MCP (`httpClient`) już zwraca błąd.

**Zmiana.** W CLI użyć tej samej funkcji co MCP: `generic.httpClient(cfg, cfg.ProfileName())`. Dzięki temu
`api raw/download/upload` przyjmują profil każdego rodzaju HTTP (`github`, `gitlab`, `jira`, `atlassian`, `api`),
a profil nieistniejący, `azure` albo `ssh-exec` daje czytelny błąd. Jeśli `--profile` podano, a nie da się go użyć,
**nigdy** nie wracamy do domyślnego.

**Test.** `--profile` rodzaju `gitlab` trafia do GitLaba; `--profile` nieistniejący i `azure` kończą się błędem;
bez `--profile` działa `defaults.api` jak dotąd.

## 7. `config show` i redakcja

**Stan.** `config show` robi `json.MarshalIndent(cfg)`. Sekrety (token, `client_secret`, `refresh_token`) są w
keyringu, więc nie wyciekają, ale w wyniku są: `oauth2.client_id`, `username`, `ssh_destination`, `commands`,
`token_env` (nazwa zmiennej), `auth_header`.

**Zmiana.** Jedna funkcja `Config.Redacted()` zwraca kopię, w której pola poświadczeń i identyfikatorów mają
wartość `"***"` (puste zostają puste, żeby było widać brak konfiguracji): `oauth2.client_id`, `token_env`,
`auth_header`, `form_login.*field`. `username` i URL-e zostają (to nie sekrety, są potrzebne do diagnozy). Flaga
`--reveal` nie powstaje; kto chce pełny plik, czyta go z dysku (`config path`).

**Do decyzji.** Czy `username` też redagować (agent może widzieć konta, ale nie musi znać adresów e-mail)?
Proponuję: nie redagować, bo `accounts` i tak je pokazuje.

**Test.** Tabela pól: żadna wartość z listy nie występuje w wyniku; struktura reszty bez zmian.

## 3. Komenda `accounts` w CLI

**Stan.** MCP ma narzędzie `accounts` (`auth.CallTool`), a CLI ma `auth status`. Brakuje krótkiej komendy, która
mówi, jakiego profilu użyć. `mr list` kończy się „several profiles match (github, gitlab)”.

**Zmiana.**
- `workctl accounts` wypisuje te same dane co narzędzie MCP (profil, rodzaj, konto, repozytoria, czy domyślny),
  przez tę samą funkcję. Brak nowej logiki.
- Błąd niejednoznaczności w `mr list` i `pr list` dostaje podpowiedź: `use --profile NAME (see: workctl accounts)`
  i listę pasujących profili.

**Test.** Wyjście CLI równe wyjściu narzędzia MCP; błąd niejednoznaczności zawiera nazwy profili.

## 6. `doctor` per profil: DNS, timeout, 401

**Stan.** `doctorCheckProfile` zwraca surowy błąd. Agent musiał zgadywać, czy winien jest DNS, sieć czy token.

**Zmiana.** Jedna funkcja `classifyError(err) string` zwraca stały kod: `dns` (`*net.DNSError`), `timeout`
(`context.DeadlineExceeded`, `net.Error.Timeout()`), `tls` (błędy certyfikatu), `unauthorized` (`api.IsStatus(err, 401)`),
`forbidden` (403), `refused` (`ECONNREFUSED`), `other`. `doctorCheck` dostaje pole `code` obok `error`. Z
`api.IsStatus` korzystamy już teraz, więc zmiana jest mała. Kod wyjścia i format reszty bez zmian.

**Test.** Tabela błędów (sztuczny DNS, deadline, 401, 403) na kody; serwer `httptest` zwracający 401.

## 1. `mr show --include` w CLI

**Stan.** MCP ma `pr_show` z `include` (`files, reviews, reviewers, comments, discussion, checks`), złożone w
`pr.showParts`. CLI `gitlab mr` ma tylko `get/notes/changes` osobno.

**Zmiana.** Nie pisać drugi raz: `workctl gitlab mr show --include a,b ID` i analogicznie `github pr show` wołają
`pr.CallTool("pr_show", ...)` z tymi samymi argumentami i drukują wynik. Nazwy części jak w MCP (jedno słownictwo).
Dodatkowo `--format raw|json|text` zgodnie z MCP.

**Ryzyko.** CLI działa dziś na typowanym kliencie, MCP na `map[string]any`; mapowanie flag na mapę to kilka
linii. Uporządkowanie argumentów (M8 z raportu prostoty) ułatwi to później, ale nie jest warunkiem.

**Test.** `--include` z komentarzami i checkami zwraca te same klucze co `pr_show`.

## 4. CLI `download` z metadanymi

**Stan.** `api download` już istnieje (`generic/download.go`, ten sam kod co narzędzie MCP `download`). Agent
użył `api raw` i `file`, bo `download` działa tylko na profilu rodzaju `api` (ten sam błąd co w punkcie 2)
i wybierał profil domyślny.

**Zmiana.** Po poprawce punktu 2 `workctl --profile NAME api download PATH` zwraca ścieżkę, rozmiar, SHA-256,
typ zawartości, nazwę pliku z serwera i **ostrzeżenie o stronie logowania zamiast pliku**. Do zrobienia tylko:
(a) upewnić się, że wynik CLI to JSON z tymi polami (jak MCP), nie sam komunikat, (b) opisać w `docs/cli.md`.

**Test.** Serwer zwracający HTML logowania z `Content-Type: text/html` dla żądania pliku: ostrzeżenie w wyniku.

## Proponowane gałęzie

1. `fix/cli-profile-selection`: punkty 2 i 4 (wspólna przyczyna), potem 3.
2. `feat/cli-diagnostics`: punkty 6 i 7.
3. `feat/cli-mr-include`: punkt 1.

Po każdym scaleniu do `main` podbicie wersji `Z` (zasada z `docs/development.md`).

## Otwarte pytania

1. Czy `username` w `config show` ma być redagowany (pkt 7)? Proponuję: nie.
