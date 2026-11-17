# Raport: co uprościć w workctl (osobny MR)

Autor: [przestaw](https://github.com/przestaw)

Status (2026-11-17): częściowo zrobione. Sześć uproszczeń (tabela kind-ów, `FetchRequest`, `DoBody`, wspólny wybór
profilu, podział `MatchResource`, `StatusOf`) jest w lokalnej gałęzi `draft/simplify-core-isolated`, niewypchniętej.
Pójdą jako osobny MR po scaleniu PR #11, bo dotykają tych samych plików. Reszta listy poniżej czeka.

Cel: kod prosty, DRY i ze wspólnymi backendami; dobry do nauki zaawansowanego Go, bez frameworków
„na zapas”. Każda zmiana nie zmienia zachowania i przechodzi `go build`, `go vet`, `go test -race` oraz `golangci-lint`.

## Co jest dobre i ma zostać

- `internal/mcp` nie zna dostawców: dostaje listę narzędzi i funkcję `Call`.
- `vcs.PullRequestReader` to mały interfejs z adapterem dla każdego dostawcy.
- Bezpieczeństwo (polityka zapisu, brak absolutnych URL-i, przekierowania) jest w jednym miejscu, w `api.Client`.
- Klient jest tworzony przy każdym wywołaniu, więc brak logowania psuje tylko jednego dostawcę.
- Tabela `github.prReads` sprawia, że CLI i MCP nie mogą się rozjechać.
- Duże pliki są przesyłane strumieniowo przez `io.Reader`.

To dobre przykłady do nauki i argument **przeciw** przepisywaniu całości.

## 1. Szybkie poprawki (małe ryzyko)

| # | Co | Gdzie | Zmiana | Efekt |
| --- | --- | --- | --- | --- |
| Q1 | 5 kopii „otwórz URL w przeglądarce” | `internal/auth/auth.go`, `azure/cmd/helpers.go`, `azure/repl/completer.go`, `atlassian/repl/completer.go`, `atlassian/cmd/issue.go` | jedna funkcja `cliutil.OpenURL` na `proc.Command` (da się podmienić w testach) | ok. −35 linii, jedno miejsce do testu |
| Q2 | Kopia `FirstNonEmpty` | `internal/api/oauth2.go` (`cliutilFirstNonEmpty`) | użyć `cliutil.FirstNonEmpty` | drobne |
| Q3 | MCP nadpisuje argument, żeby przekazać stan | `internal/pr/pr.go:184` (`a["format"] = "json"`) | liczyć format w zmiennej lokalnej | brak ukrytego efektu ubocznego |
| Q4 | `Fetch` z 9 parametrami pozycyjnymi | `internal/pr/pr.go:87` | struktura `FetchRequest{Repo, Project, Search, State, ID}` | czytelne wywołania |
| Q5 | Adapter `BodyOnly` w 27 miejscach | `internal/api/client.go` i wywołania | metoda `DoBody(...) ([]byte, error)` obok `Do` | mniej opakowywania |
| Q6 | Duże `switch` po nazwie narzędzia MCP | `internal/github/tools.go`, `gitlab/tools.go`, `jira/tools.go` | tabela `nazwa → handler` jak w `prReads`; wspólna funkcja do wyboru profilu i klienta | −15…30 linii na dostawcę, ten sam wzorzec wszędzie |
| Q7 | `context.Background()` w środku komend | np. `internal/pr/pr.go:55` | kontekst przekazywany z `app` | poprawne anulowanie (Ctrl-C), prostsze testy |

## 2. Średnie refaktory (warto, ostrożnie)

| # | Co | Gdzie | Zmiana | Ryzyko |
| --- | --- | --- | --- | --- |
| M1 | Wiedza „który kind co umie” rozsiana po pakietach | `config.PRKinds` (`config.go:279`), `generic.httpKinds` (`generic.go:117`), `switch` w `pr`, `app` i `doctor` | **jedna tabela w `config`**: `kinds = map[string]KindInfo{HTTP, PR, Issues bool}`. Dodanie `google` to wtedy jeden wpis | średnie; **zrobić przed fazą 1** |
| M2 | Wybór profilu w czterech wariantach | `config.go` (`ProfileOf`, `PRProfile`, `PRProfileFor`, `matchProfile`) i `generic.httpClient` | jedna funkcja `Resolve(kind, resource) (name, Profile, error)` | średnie |
| M3 | `MatchResource` robi wszystko naraz (Jira key, wildcard repo, aliasy GitLab, punktacja) | `config.go:381` | mała funkcja dopasowania dla każdego rodzaju zasobu i jedna punktacja | średnie |
| M4 | Dwie ścieżki strumieniowe: download i nowy upload | `api/download.go`, `api/client.go` (`StreamReader`) | wspólne budowanie żądania i obsługa odpowiedzi; resumable zostaje w kodzie Drive | średnie |
| M5 | Pętle stronicowania w kilku miejscach | `api/pages.go`, `issue/issue.go`, `atlassian/client/search.go` | jedna generyczna funkcja `collect[T](next func(page) ([]T, more bool, error))`; generyki mają tu sens | średnie |
| M6 | Błędy HTTP formatowane na różne sposoby | `api/client.go`, klienci azure i atlassian | jeden typ `*api.Error` sprawdzany przez `errors.As`, np. `IsStatus(err, 401)` (przyda się przy ponowieniu po 401 w OAuth) | średnie |
| M7 | Duplikat keyringa w obu wbudowanych CLI | `azure/config/config.go:153`, `atlassian/config/config.go:162` | użyć `internal/secret` z parametrem nazwy konta | średnie |
| M8 | `map[string]any` aż do logiki | `github/tools.go`, `gitlab/tools.go`, `pr/pr.go` | mapa tylko na granicy MCP, potem typowana struktura argumentów | średnie; robić stopniowo |

## 3. Większe pomysły: oceniam, że **nie warto teraz**

| Pomysł | Dlaczego nie |
| --- | --- |
| Rejestr dostawców z fabrykami klientów i narzędzi | Dostawców jest pięciu, a `switch` jest czytelny. Tabela z M1 wystarczy. |
| Wspólne API dla issue, PR, komentarzy i checków | Semantyka dostawców za bardzo się różni; wyszłoby przeinżynierowane. |
| Wspólny framework dla CLI azure i atlassian | Duże, osobno rozwijane drzewa komend. Współdzielić tylko pomocnicze funkcje (Q1, M6, M7). |
| Rozbicie `api.Client` na warstwy (polityka, transport) | Dobre do nauki, ale ryzykowne. Wrócić po fazie 1, gdy OAuth i Google będą stabilne. |

## Proponowana kolejność w MR

1. Q1, Q2, Q3, Q5 (mechaniczne, osobne commity).
2. M1 (tabela kindów), potem M2.
3. Q4, Q6, Q7.
4. M4–M8 według potrzeby, każde osobno.

Odrzuciłem z przeglądu: zamianę `map[string]bool` na `map[string]struct{}` (obecny zapis jest idiomatyczny)
oraz typ dla metod HTTP (`net/http` ma stałe, to wystarcza).
