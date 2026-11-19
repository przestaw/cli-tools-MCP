# Plan: OAuth, profile Google (gdrive / gcloud / google) i opcjonalny OAuth dla GitHub / GitLab

Autor: [przestaw](https://github.com/przestaw)

Status (2026-11-19, wersja 0.4.7): fazy 0, 1 i 2b są na `main`: OAuth, `auth login` i kontrola uploadu (PR #9),
`kind: "google"` (gdrive / gcloud / google) z hostami Sheets i Docs (PR #10), Drive w istniejących narzędziach (PR #14).
Faza 2b była sprawdzona na prawdziwym Drive oraz odczytami Sheets i Docs i jednym cofniętym zapisem komórki A1; inne
zapisy do Sheets i Docs oraz komentarze na Dysku nie były testowane. Otwarte: faza 2 (Colab API), 3 (device flow,
blokada tokenu) i 4 (OAuth dla GitHuba i GitLaba). Decyzja D3 nie jest potwierdzona.

## Cel

1. `workctl auth login` przez przeglądarkę. Token trafia do keyringa i sam się odświeża.
2. Trzy profile Google na **jednym** backendzie:
   - `gdrive`: tylko Drive,
   - `gcloud`: API chmurowe,
   - `google`: wszystkie API Google, w tym Colab API (stan runtime'ów).
3. OAuth dla GitHuba i GitLaba jako **dodatkowa opcja**. PAT i `gh`/`glab` zostają domyślne.

Poza zakresem:

- **Colab w przeglądarce** (`tools/colab_ui.py`, CDP, utrzymywanie sesji): niezależny od workctl.
- **Azure DevOps przez Entra ID.** Azure zostaje na PAT.
- Jira/Confluence OAuth, konta serwisowe Google, OTRS.

## Zasada projektowa: mało kodu, jeden backend

workctl ma być czytelnym przykładem zaawansowanego Go, a nie zbiorem wyjątków. Dlatego:

- **Jeden mechanizm OAuth** (`internal/api/oauth2.go`) dla wszystkich dostawców. Różnice między dostawcami to
  **dane** (tabela presetów), nie osobne gałęzie kodu.
- **Jeden kind `google`**, a w nim **tabela usług**. Trzy profile różnią się jednym polem `service`, nie kodem.
- **Bez nowych narzędzi MCP.** Wystarczą istniejące `request`, `download` i `upload`.
- **Bez heurystyk po URL-u** (dziś Drive jest rozpoznawany po końcówce `/drive/v3` w trzech miejscach).
- Kod specyficzny dla Drive (upload resumable, `/about`) przenosimy z `generic` do `internal/google`.
  `generic` znów jest generyczny.

Szkic tabeli (jedyne miejsce, które wie, czym różnią się profile Google):

```go
type service struct {
    BaseURL string   // domyślny url profilu
    Hosts   []string // hosty, na które może iść token
    Scopes  []string // domyślne scope'y przy logowaniu
    WhoAmI  string   // ścieżka do sprawdzenia konta
}

var services = map[string]service{
    "drive": {"https://www.googleapis.com/drive/v3", []string{"www.googleapis.com"}, []string{scopeDrive}, "/about?fields=user"},
    "cloud": {"https://cloudresourcemanager.googleapis.com", []string{"*.googleapis.com"}, []string{scopeCloud}, ""},
    "all":   {"https://www.googleapis.com", []string{"*.googleapis.com"}, nil /* z configu */, ""},
}
```

## Profile Google

| Profil | `service` | Gdzie może iść token | Scope'y | Do czego |
| --- | --- | --- | --- | --- |
| `gdrive` | `drive` | tylko `www.googleapis.com` (Drive) | `drive` | checkpointy, wheele, notebooki, wyniki |
| `gcloud` | `cloud` | `*.googleapis.com` | `cloud-platform` | GCE, Vertex/Colab Enterprise, storage |
| `google` | `all` | `*.googleapis.com` | z configu, czyli tyle, ile nada klucz | Colab API, Drive, cokolwiek innego |

Dwie niezależne granice:

- **Hosty** to granica po stronie workctl. `gdrive` nie wyśle tokenu nigdzie poza Drive, nawet jeśli
  token ma szersze uprawnienia (stąd „nieważne, jakie uprawnienia nadane”).
- **Scope'y** to granica po stronie Google. `doctor` pokazuje, jakie scope'y faktycznie nadano.

Config (sekrety nie trafiają do pliku):

```json
"gdrive": { "kind": "google", "service": "drive",  "username": "przestaw@gmail.com", "oauth2": { "client_id": "…" } },
"gcloud": { "kind": "google", "service": "cloud",  "username": "przestaw@gmail.com", "oauth2": { "client_id": "…" } },
"google": { "kind": "google", "service": "all",    "username": "przestaw@gmail.com",
            "oauth2": { "client_id": "…", "scopes": ["https://www.googleapis.com/auth/colaboratory.readonly",
                                                     "https://www.googleapis.com/auth/drive.readonly"] } }
```

Endpointy OAuth Google (`auth_url`, `token_url`) i parametry `access_type=offline`, `prompt=consent` pochodzą
z presetu `google`. Nie wpisuje się ich do profilu.

Każdy profil loguje się osobno (`auth login --profile X`) i ma własny refresh token, bo ma inne scope'y.
Profile mogą korzystać z tego samego klienta OAuth (`client_id`), ale `client_secret` trzeba zapisać dla każdego
profilu (albo podać przy pierwszym logowaniu).

### Colab API (w profilu `google`)

Usługa `colaboratory.googleapis.com`, scope `colaboratory.readonly` (do odczytu) lub `colaboratory`:

| Wywołanie | Co daje | Zapis? |
| --- | --- | --- |
| `GET /v1beta/runtimes` | runtime'y przydzielone użytkownikowi | nie |
| `GET /v1beta/runtimes/{id}` | jeden runtime | nie |
| `GET /v1beta/runtimespecs` | dostępne maszyny i czy przysługują | nie |
| `GET /v1beta/subscription` | plan (Pro / Pay-as-you-go) | nie |
| `GET /v1/operations/…` | stan długich operacji | nie |
| `POST /v1beta/runtimes`, `DELETE …` | przydziela / zwalnia runtime (**kosztuje**) | tak, wymaga `allow_write` |

To API zwraca **stan runtime'u** (czy żyje, jaka maszyna), a nie stan komórek notebooka. Postęp eksperymentu nadal
odczytujemy z plików na Drive. API jest w wersji `v1beta`, więc może się zmienić.

Przykład: `workctl --profile google request GET https://colaboratory.googleapis.com/v1beta/runtimes`.
Absolutny URL jest dozwolony tylko dla hostów z tabeli usług danego profilu.

## Drive w istniejących narzędziach (bez nowych narzędzi MCP)

Zasada: **żadnych nowych nazw narzędzi** (agent czyta listę przy każdej rozmowie, a GitLens i GitKraken już ją
wypełniają). Operacje Dysku trafiają do istniejących narzędzi, wybieranych po profilu (`HasDrive()`), a kod mieści
się w `internal/google`. Zasady zapisu, lista hostów i klient są wspólne z resztą.

| Potrzeba | Narzędzie | Zmiana |
| --- | --- | --- |
| konto i quota | `accounts` | nowy opcjonalny parametr `check`: dla profili `google` i `gdrive` jedno `GET /about?fields=user,storageQuota`, wynik dopisany do wpisu (`user`, `quota_used`, `quota_limit`). Bez `check` bez zmian i bez sieci. Dla `service: cloud` brak `/about`, więc tylko nadane scope'y z odpowiedzi tokenu. |
| list i search | `file` | profil Dysku: pusty `path` lub ID folderu wypisuje zawartość (`id`, `name`, `mimeType`, `size`, `modifiedTime`, `md5Checksum`); nowe opcjonalne `query` (nazwa zawiera, wstawiana do `q=` z ucieczką apostrofów) i `limit` (domyślnie 30). |
| metadane pliku | `file` i `download` | `file` z ID pliku zwraca metadane; `download` dopisuje do wyniku blok `drive` (`id`, `name`, `mimeType`, `modifiedTime`, `md5Checksum`, `webViewLink`) i `md5_verified` (zgodność z sumą z Dysku, obok SHA-256, który już jest). |
| eksport natywnych plików | `download` | opcjonalne `export_mime`. Bez niego: Docs → `text/plain`, Sheets → `text/csv` (**tylko pierwszy arkusz**), Slides → `application/pdf`. Limit eksportu Drive to 10 MB; błąd wypisuje dostępne formaty. |
| zaawansowane zapytania | `request` | bez zmian (`q`, `fields`, `orderBy` wprost) |

Uwagi:

- Dziś `file` czyta plik z repozytorium (GitHub/GitLab) i **nie szuka**; wyszukiwanie dla tych dostawców jest w innych
  narzędziach. `query` i `limit` mają więc sens tylko dla Dysku i dla innych profili dają błąd „nie dotyczy”.
  Opis narzędzia pozostaje krótki: „plik z repozytorium lub z Dysku Google (profil gdrive/google)”.
- `file` wybiera dostawcę po rodzaju profilu repozytorium (`resolveRepo`), więc trzeba dodać gałąź „profil Dysku”
  przed tym wyborem. Nazwa zamiast ID: tylko w folderze (`default_project` lub `folder`) i tylko gdy jest
  jednoznaczna; inaczej błąd z listą kandydatów (nazwy na Dysku się powtarzają).
- Treść w `file` dla pliku tekstowego mieści się w `max_output_bytes`; większe: tylko metadane i podpowiedź,
  żeby użyć `download`. **Do decyzji (D6).**
- CLI: te same funkcje. `workctl accounts --check` (patrz [plan-luki-cli.md](plan-luki-cli.md), punkt 3) oraz
  `api download --export-mime`. Osobne `drive ls` nie powstaje.

### Sheets i Docs API wprost

`sheets.googleapis.com` i `docs.googleapis.com` przyjmują zakres `drive`, więc dodatkowe zakresy nie są potrzebne.
Trzeba tylko (a) włączyć te API w projekcie klienta OAuth i (b) pozwolić na hosty.

| Profil | Dziś | Propozycja |
| --- | --- | --- |
| `google` (`all`) | `*.googleapis.com`, działa po włączeniu API | bez zmian |
| `gdrive` (`drive`) | tylko `www.googleapis.com`, Sheets i Docs odrzucone | dodać `sheets.googleapis.com` i `docs.googleapis.com` do hostów presetu `drive` |

Token nadal ma zakres `drive`, więc dodane hosty nie dają nowych uprawnień; zmienia się tylko, dokąd workctl wyśle
token (nadal tylko API dokumentów Google związanych z Dyskiem). To lekko zmienia obietnicę „gdrive tylko Drive”,
dlatego jest osobną decyzją (D5). Alternatywa: osobna usługa `service: workspace`, ale to kolejny profil do
utrzymania, więc nie polecam. Slides i inne API tylko w `google`. Zapisy (np. `values:append`, `documents:batchUpdate`)
nadal wymagają `policy.allow_write`.

### Dokumentacja (człowiek i agent), razem z kodem

| Odbiorca | Gdzie | Co |
| --- | --- | --- |
| człowiek | `docs/services-auth.md` | sekcja `kind: "google"`: parametry, tabela hostów (z Sheets i Docs), włączanie API w Cloud Console, przykłady `accounts check`, listy i eksportu |
| człowiek | `docs/cli.md` | `accounts --check`, `api download --export-mime` |
| agent | opisy narzędzi w schemacie MCP (`file`, `download`, `accounts`) | to jedyna dokumentacja, którą agent czyta zawsze; krótko i jednoznacznie |
| agent | `docs/mcp.md` i `plugins/workctl/skills/workctl/SKILL.md` | tabela parametrów, przepis „znajdź plik na Dysku, sprawdź sumę, pobierz” |
| agent | skill `colab-compute` (poza repo) | użycie `file` i `download` do monitorowania wyników na Dysku zamiast `api raw` |

## Kim jest „aplikacja” przy OAuth (GitHub / GitLab)

**Krótko: przy OAuth logujesz się nadal jako ty (`przestaw`, `pstawczyk`, `stawczyk`). Aplikacja tylko
przedstawia się na ekranie zgody.**

Każde logowanie OAuth ma dwie tożsamości:

| | Kto to | Co widzi serwer |
| --- | --- | --- |
| **klient** (`client_id`) | program, który prosi o token, tu „workctl” | nazwa na ekranie zgody, lista „Authorized applications”, audit log organizacji |
| **użytkownik** | konto, które kliknęło „Authorize” | **autor każdej akcji** (komentarz, PR, odczyt) |

Token z logowania OAuth to **token użytkownika**. Może tyle, ile może konto, i tylko w zakresie zatwierdzonych
scope'ów. Dokładnie tak działa `gh auth login`, bo `gh` to aplikacja OAuth „GitHub CLI”. Commity i komentarze są
podpisane twoim loginem, a nie „GitHub CLI”.

**„Logowanie jako aplikacja”, które brzmi ślisko, to coś innego** i tego **nie** robimy w MCP:

| Rodzaj tokenu | Działa jako | Dla MCP? |
| --- | --- | --- |
| PAT (dziś) | ty | tak |
| token OAuth użytkownika (ten plan) | ty, „przez workctl” | tak |
| token instalacji GitHub App, token grupy/projektu GitLab, konto bota | **bot**, nie ty | **nie** |

MCP to prawa ręka agenta, a agent to prawa ręka zalogowanego usera. Każda akcja musi mieć autora-człowieka i jego
uprawnienia, więc w MCP dozwolone są tylko tokeny użytkownika. Tokeny bota ewentualnie kiedyś dla wspólnego
dashboardu zespołu, w osobnym profilu i nigdy przez MCP.

**Kilka kont:** każde konto to osobny profil z osobnym tokenem i przypiętym `username`. Ten sam `client_id` może
obsłużyć wiele kont na tym samym hoście. Jeśli zalogujesz się na złe konto, workctl to wykryje i zablokuje zapis.

**Skąd `client_id` (decyzja D3):**

- **github.com:** rejestrujesz jedną aplikację OAuth „workctl” na koncie `przestaw` i włączasz device flow.
  `client_id` jest publiczny, więc można go wkompilować. Organizacje z „OAuth App access restrictions” muszą
  ją raz zatwierdzić; inaczej zostaje PAT.
- **GitLab firmowy:** aplikację rejestruje się na tej instancji. Może to zrobić każdy użytkownik
  (Preferences → Applications) albo admin jako aplikację instancji (bez ekranu zgody). `client_id` wpisujesz
  w profil.
- **Nie** używamy `client_id` należącego do `gh`, `glab` ani innego programu, bo to podszywanie się pod cudzą
  aplikację.

**Co OAuth daje w porównaniu z PAT:** nie trzeba ręcznie tworzyć ani przedłużać tokenów, tokeny GitLaba wygasają
po 2 h i same się odświeżają (mniejsza szkoda przy wycieku), a dostęp odwołuje się jednym kliknięciem
w „Authorized applications”. Ryzyko po stronie MCP jest takie samo jak przy PAT o tych samych scope'ach.

**Homepage „na początek dnia”** (pipeline'y, MR-y, Jira, OTRS) to ten sam model: CLI z tokenami użytkownika
i scope'ami tylko do odczytu, osobne profile `*-ro` z `read_only: true`.

## Poprawki fazy 0 (wdrożone w PR #9)

Lista poniżej to zakres fazy 0 z chwili planowania. Zakres wszedł na `main` w PR #9; w kodzie sprawdzone są punkty 1
(`--overwrite` w uploadzie) i 3 (ponowna próba po 401). Pozostałych punktów nie sprawdzano osobno przy tej aktualizacji.

Blokowało merge:

1. Upload pliku o nazwie, która już jest w folderze, **nadpisuje go bez pytania**. Bez `--overwrite` ma się nie udać.
2. Uruchomić build, testy i lint.

Do poprawy:

3. Brak ponownej próby po 401: wyczyścić cache, odświeżyć token i spróbować raz jeszcze.
4. Parametry Google wpisane na sztywno w ogólnym logowaniu. Przenieść do presetu.
5. `set-token` nie czyści cache tokenu.
6. Brak sprawdzenia konta (`username`) po logowaniu i przed zapisem.
7. Aplikacja OAuth Google w trybie „Testing” daje refresh token ważny 7 dni. Przełączyć na „In production”,
   a `doctor` ma czytelnie zgłaszać wygaśnięcie.
8. Odpowiedź `callback` z błędem nie sprawdza `state`.

## Fazy

| Faza | Co | Kiedy gotowe |
| --- | --- | --- |
| **MR uproszczeń** | (**zrobione**, PR #16) osobny MR według [raport-prostota-kodu.md](raport-prostota-kodu.md), szybkie poprawki przed fazą 1 | build, testy i lint zielone; zachowanie bez zmian |
| **0** | poprawki 1–8 (**zrobione**, PR #9) | logowanie, `doctor`, pobieranie i upload do Drive działają; brak cichego nadpisywania |
| **1** | (**zrobione**, PR #10) `kind: "google"` z tabelą usług, presety OAuth jako dane, lista hostów zamiast heurystyki URL, przeniesienie kodu Drive do `internal/google` | trzy profile działają; `gdrive` odmawia wysłania tokenu poza Drive (test) |
| **2** | (**otwarte**) Colab API w profilu `google`: `doctor` pokazuje runtime'y i nadane scope'y, dokumentacja przykładów | `request` na `/v1beta/runtimes` działa; `POST`/`DELETE` blokowane bez `allow_write` |
| **2b** | (**zrobione**, PR #14) Drive w istniejących narzędziach (`accounts check`, `file` list/search/metadane, `download` z metadanymi i eksportem), hosty Sheets i Docs w `gdrive`, dokumentacja | trzy narzędzia działają na profilu Dysku; testy na serwerze `httptest`; bez nowych narzędzi MCP |
| **3** | (**otwarte**) wspólny rdzeń OAuth dla kolejnych dostawców: device flow, zapis nowego refresh tokenu z blokadą między procesami | testy każdego flow; dwa procesy naraz nie psują tokenu |
| **4** | (**otwarte**) GitHub i GitLab: `auth: "oauth2"` jako opcja; `setup-git` oddaje token OAuth | `pr_list` działa na profilu OAuth; profile PAT i `cli` bez zmian |
| **5** | dokumentacja (`services-auth.md`, `config.example.json`, konfiguracja Google Cloud Console) | częściowo w PR #9, #10 i #14 |

Faza 0 w trzech commitach: (a) OAuth + `auth` + `doctor`, (b) upload, (c) dokumentacja.

## Decyzje

| # | Pytanie | Stan |
| --- | --- | --- |
| D1 | Jak oznaczyć profil Google? | **przyjęte:** `kind: "google"` + `service: drive / cloud / all` |
| D2 | Colab w workctl? | **przyjęte:** tylko Colab API (odczyt stanu); przeglądarkowy Colab poza workctl |
| D3 | Czyj `client_id`? | **do potwierdzenia:** własna aplikacja per host, tylko tokeny użytkownika, w MCP nigdy tokeny bota |
| D4 | Azure / Entra | **poza zakresem** |
| D5 | Sheets i Docs w hostach `gdrive`? | **wdrożone (PR #10):** hosty w presecie `drive`; API trzeba włączyć w projekcie klienta OAuth. Sprawdzone odczytami na żywo i jednym cofniętym zapisem komórki A1; inne zapisy nie były testowane |
| D6 | `file` zwraca treść pliku tekstowego, czy tylko metadane? | **wdrożone (PR #14) w wariancie zawężonym:** `file` daje metadane lub listę, treść przez `download` (z `export_mime` dla dokumentów natywnych) |

## Ryzyka

- **Logowanie Google wygasa podczas długiego runu.** Zabezpieczenie: punkt 7, a monitor zgłasza błąd logowania
  zamiast „brak nowych plików”.
- **Colab API jest w `v1beta`** i może się zmienić. Używamy go tylko przez `request`, więc nie ma kodu
  do przepisywania.
- **`*.googleapis.com` to szeroka lista hostów.** Dlatego `gdrive` jej nie używa, a szerokie profile mają
  minimalne scope'y.
- **Organizacja blokuje aplikację OAuth.** Wtedy zostaje PAT albo `gh`/`glab`.
