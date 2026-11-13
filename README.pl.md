# workctl

[English](https://github.com/przestaw/cli-tools-MCP/blob/main/README.md) · **Polski**

[![CI](https://github.com/przestaw/cli-tools-MCP/actions/workflows/ci.yml/badge.svg)](https://github.com/przestaw/cli-tools-MCP/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8)](https://go.dev/dl/)
[![Licencja](https://img.shields.io/badge/license-BSD--4--Clause-blue)](https://github.com/przestaw/cli-tools-MCP/blob/main/LICENSE)

**Połącz swoje usługi z agentami przez API — jeden CLI i serwer MCP.**

workctl daje lokalnym asystentom programistycznym kontrolowany dostęp do usług przez konfigurowalne API HTTP
i dozwolone polecenia SSH. Używaj go tam, gdzie potrzebujesz danych lub operacji usługi: w programowaniu,
monitoringu, CI/CD, obsłudze pakietów, deploymentach, administracji czy własnych narzędziach wewnętrznych.

Nazwane integracje dostarczają wygodnych poleceń, nie wyznaczają zamkniętej listy zgodnych usług.
Zapis jest domyślnie wyłączony; profile jawnie określają adresy i poświadczenia, a opcjonalne zgody
pozwalają przeglądać zmiany pojedynczo lub batchami.

[Szybki start](#szybki-start) · [Konfiguracja MCP](#mcp-client) · [Zatwierdzanie zapisów](#write-approvals) · [Dokumentacja](#dokumentacja)

## Możliwości

- **Jeden plik wykonywalny Go:** klient wiersza poleceń i serwer [Model Context Protocol](https://modelcontextprotocol.io/) przez stdio.
- **Zwięzłe odpowiedzi:** wspólny format PR-ów i zgłoszeń; recenzje, pliki, dyskusje i checki pobierane na żądanie. Dostępny jest też surowy wynik.
- **Jawna polityka zapisu:** zapis wymaga włączenia; opcjonalne potwierdzenia dotyczą operacji, batcha albo połączenia MCP.
- **Dobór konta:** profil wybierany według repozytorium/projektu, jawnej nazwy lub wartości domyślnej. Powiązanie konta z natywnym profilem GitHub/GitLab pozwala sprawdzać token przed zapisem.
- **Poświadczenia poza repozytorium:** systemowy pęk kluczy; dla innych środowisk jawne zmienne środowiskowe lub backend CLI dostawcy.
- **Integracje agentów:** instalator i wspólny skill dla VS Code, Claude Code i Codex.
- **Operacje poza repozytoriami kodu:** odpytywanie API monitoringu, sprawdzanie deploymentów i wywoływanie endpointów zarządzania przez profile usług.

## Dwie drogi podłączenia usługi

| Sposób | Kiedy go użyć |
|---|---|
| Skonfigurowane API HTTP lub dozwolone polecenia SSH | Podłącz istniejącą usługę bez tworzenia dedykowanego konektora. Podaj adres, obsługiwane uwierzytelnianie i operacje potrzebne w Twoim procesie pracy. |
| Wbudowane polecenia dostawców | Korzystaj z wygodnych poleceń i wspólnych formatów odpowiedzi tam, gdzie są dostępne, m.in. dla GitHuba, GitLaba, Jiry, Confluence i Azure DevOps. |

O zgodności decydują protokół, uwierzytelnianie i semantyka endpointów, a nie obecność marki na liście.
Ogólny dostęp nie oznacza gotowej integracji ani lokalnego klienta każdego produktu.
Serwer udostępnia narzędzia zgodnie z konfiguracją backendów.

**Przykłady, nie granica obsługi:** Jenkins i usługi Conan były z powodzeniem używane przez autora
przez skonfigurowane profile API. Ten sam sposób łączenia dotyczy zgodnych API monitoringu, deploymentu,
zarządzania i usług własnych; każde wymaga odpowiedniej konfiguracji.
Dostęp do serwera Conan nie zastępuje lokalnego rozwiązywania zależności i budowania przez Conan CLI.

### Dowolne API, monitoring i zarządzanie usługami

Narzędzie `request` nie jest ograniczone do wbudowanych integracji. Skonfiguruj profil `kind: "api"` z adresem usługi
i uwierzytelnianiem, a następnie podaj endpoint, metodę HTTP, parametry zapytania i body wymagane przez tę usługę.
Pełne URL-e żądań są odrzucane: profil określa serwer docelowy i jego poświadczenie.

Przykładowo, po skonfigurowaniu profilu `monitoring` dla API zgodnego z Prometheusem, wywołaj `request` z argumentami:

```json
{
  "profile": "monitoring",
  "method": "GET",
  "path": "/api/v1/query",
  "query": {"query": "up"}
}
```

Tym samym mechanizmem można sprawdzić usługę deployment, uruchomić build Jenkinsa, odpytać repozytorium
artefaktów lub obsłużyć konsolę zarządzania. To konfigurowalne operacje API, nie deklaracja gotowego konektora
do każdego produktu. Użytkownik dobiera semantykę endpointów i uprawnienia; żądania inne niż odczyt wymagają
`allow_write` i skonfigurowanego trybu zgody. Dla profili monitoringu bez prawa zapisu ustaw `read_only: true`.

[Profile usług i uwierzytelnianie](https://github.com/przestaw/cli-tools-MCP/blob/main/docs/services-auth.md)
opisują bearer/basic, własny nagłówek, logowanie formularzem oraz dostęp przez SSH.

## Szybki start

Wymagania: **Go 1.26+**, **Python 3** do instalatora, Git i dostęp do konfigurowanych usług.
Jeżeli środowisko tego wymaga, zamiast `python` użyj `python3`.

```sh
git clone https://github.com/przestaw/cli-tools-MCP.git
cd cli-tools-MCP
python scripts/install.py --vscode
workctl github init --profile personal
workctl auth status
```

Dla innych integracji wybierz `--claude`, `--codex` lub `--all` zamiast `--vscode`.
Bez flagi integracji instalator buduje program bez rejestrowania klienta MCP.
Instaluje go w `~/.local/bin`, może zmienić PATH użytkownika i próbuje przenieść starsze wpisy pęku kluczy.
Po instalacji lub aktualizacji zrestartuj serwer MCP w kliencie, który go uruchamia.

Pozostałe usługi skonfiguruj przez `workctl init` albo uruchom konfigurację tylko potrzebnego dostawcy.
Tokeny wpisuje się w formularzu konfiguracji, nie w argumentach polecenia.
Podczas sprawdzania konta i wyboru repozytorium pozostaw zapis wyłączony:

```sh
workctl --profile personal github pr list --repo owner/repository
workctl --profile personal github pr get 42 --repo owner/repository
```

<a id="mcp-client"></a>

## Podłączenie klienta MCP

Zgodny klient MCP obsługujący stdio może uruchomić `workctl mcp`. Typowa konfiguracja:

```json
{
  "mcpServers": {
    "workctl": {
      "command": "workctl",
      "args": ["mcp"]
    }
  }
}
```

Jeśli program nie jest dostępny w PATH klienta, podaj jego pełną ścieżkę. VS Code używa obiektu `servers`
i pola `type: "stdio"`; instalator obsługuje ten format. Konfiguracja i interfejs potwierdzeń zależą od hosta:
zobacz [przewodnik MCP](https://github.com/przestaw/cli-tools-MCP/blob/main/docs/mcp.md).
Powyższa konfiguracja MCP nie zawiera poświadczeń.

Agent może wywołać `pr_show` z poniższymi argumentami, aby sprawdzić PR bez jego zmieniania:

```json
{
  "profile": "personal",
  "repo": "owner/repository",
  "number": 42,
  "include": ["reviews", "checks"]
}
```

<a id="write-approvals"></a>

## Zatwierdzanie zapisów

W sekcji `policy` konfiguracji workctl świadomie wybierz zakres zgody:

| Ustawienie | Działanie |
|---|---|
| `allow_write: false` | Domyślnie: odmowa zapisu. |
| `allow_write: true` | Włączenie zapisu; samo w sobie **nie** wymaga potwierdzenia. |
| `confirm_writes: true` | Pytanie przed każdym zapisem lub jedno dla dokładnej listy operacji batcha. |
| `confirm_session_writes: true` | Pytanie przed pierwszym zapisem; zgoda obejmuje kolejne zapisy do skonfigurowanych profili w tym połączeniu MCP. |

Oba tryby potwierdzeń są domyślnie wyłączone i nie mogą być aktywne równocześnie. Ostrożny wariant to
poniższy fragment włączony do istniejącej konfiguracji; zachowaj jej profile i pozostałe ustawienia:

```json
{
  "policy": {
    "allow_write": true,
    "confirm_writes": true
  }
}
```

Dla znanego zestawu operacji użyj `batch`, dodając krótkie wyjaśnienie każdej pozycji:

```json
{
  "calls": [
    {
      "tool": "pr_edit",
      "description": "Zmień tytuł PR #42, aby opisywał poprawkę paginacji",
      "args": {
        "profile": "personal",
        "repo": "owner/repository",
        "number": 42,
        "title": "Handle exact pagination boundaries"
      }
    }
  ],
  "confirm": true,
  "stop_on_error": true
}
```

Batch przyjmuje 1–20 zapisów. Wszystkie pozycje są sprawdzane lokalnie przed wykonaniem; błąd zdalny
może jednak pozostawić częściowo wykonany batch. Wcześniejsze zapisy nie są cofane. Opisy wyjaśniają
zamiar, ale podstawą zgody pozostają wyświetlone parametry żądania; długie wartości są skracane.

`confirm: true` żąda szczegółowego potwierdzenia także po zgodzie sesyjnej. Jeśli potwierdzenie jest wymagane,
a klient MCP nie potrafi go wyświetlić, workctl odmawia zapisu. Anulowane wywołanie nie powinno być wykonane.
Zgoda sesyjna trwa do końca połączenia MCP, niekoniecznie do końca tury lub rozmowy.

**Granica ochrony:** te prompty dotyczą wywołań MCP. Bezpośrednie polecenia terminala nie pytają w ten
sposób, a workctl nie zablokuje agentowi niezależnego shella, `gh` ani `curl`. W razie potrzeby ogranicz
te narzędzia w hoście. Po odmowie agent powinien się zatrzymać, a nie zmieniać transport, konto lub politykę.

## Przegląd narzędzi

| Cel | Narzędzia MCP |
|---|---|
| Konta i wybór profilu | `accounts` |
| Pull/merge requesty | `pr_list`, `pr_show`, `pr_create`, `pr_edit` |
| Zgłoszenia | `issue_show`, `issue_search`, `issue_update`, `task_get_context` |
| CI i pliki źródłowe | `ci`, `file` |
| Komentarze i multimedia | `comment`, `upload`, `download` |
| Grupowanie zapisów | `batch` |
| Inne operacje dostawców | `request`, `api_exec`, `cli` |

Nadawaj poświadczeniom tylko niezbędne uprawnienia. Nie umieszczaj tokenów w promptach ani plikach repozytorium.
[Instrukcja konfiguracji](https://github.com/przestaw/cli-tools-MCP/blob/main/docs/setup.md) opisuje dobór profilu,
pęk kluczy i jawne nadpisania przez zmienne środowiskowe.

## Dokumentacja

| Temat | Przewodnik |
|---|---|
| Polecenia CLI i polityka | [Opis CLI](https://github.com/przestaw/cli-tools-MCP/blob/main/docs/cli.md) |
| Rejestracja klienta, narzędzia i zgody | [Przewodnik MCP](https://github.com/przestaw/cli-tools-MCP/blob/main/docs/mcp.md) |
| Profile i poświadczenia | [Konfiguracja](https://github.com/przestaw/cli-tools-MCP/blob/main/docs/setup.md) |
| API, SSH i logowanie formularzem | [Inne usługi](https://github.com/przestaw/cli-tools-MCP/blob/main/docs/services-auth.md) |
| Pola konfiguracji | [Przykładowy config](https://github.com/przestaw/cli-tools-MCP/blob/main/config.example.json) |
| Organizacja kodu | [Architektura](https://github.com/przestaw/cli-tools-MCP/blob/main/ARCHITECTURE.md) |

## Rozwój i współpraca

Odtwarzalne problemy i propozycje konkretnych zmian zgłaszaj przez
[GitHub issues](https://github.com/przestaw/cli-tools-MCP/issues).
Podaj wersję workctl/klienta, dostawcę i oczyszczoną instrukcję odtworzenia.
Nie załączaj tokenów, cookies, zrzutów kont ani prywatnych danych klientów.

```sh
go build ./...
go vet ./...
go test ./...
golangci-lint run
```

CI sprawdza Windows, Linux (w tym testy race) i lint.
Szczegóły budowania i wersjonowania: [notatki deweloperskie](https://github.com/przestaw/cli-tools-MCP/blob/main/docs/development.md).

## Licencja

[BSD 4-Clause](https://github.com/przestaw/cli-tools-MCP/blob/main/LICENSE).
Copyright © 2026 [przestaw](https://github.com/przestaw).
Warunki redystrybucji i obowiązek informacji w materiałach reklamowych opisują licencja oraz
[NOTICE](https://github.com/przestaw/cli-tools-MCP/blob/main/NOTICE).
