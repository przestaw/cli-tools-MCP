# Plan: kompresja odpowiedzi narzędzi

Autor: [przestaw](https://github.com/przestaw)

Status (2026-11-19, wersja 0.4.7): część wdrożona w PR #15 (`output: compact`, projekcja szczegółów PR) i PR #12
(opcjonalny skill RTK). Pozostałe punkty są świadomie niezrobione; kryteria ich podjęcia są niżej. Opis dla użytkownika:
[compaction.md](../compaction.md) i [rtk.md](../rtk.md).

## Cel

Mniej tokenów z odpowiedzi narzędzi, bez zmiany tego, co serwer dostaje. Zasady:

- kompresja jest lokalna i dzieje się po wywołaniu HTTP,
- jest opcjonalna (`output: compact`); wyjątkiem jest krótka projekcja `details` w `pr_list` i `pr_show`, z wyjściem `raw`,
- tekst mówi wprost, co ginie. Kompresja HTML jest stratna i nie usuwa sekretów ze strony.

## Co jest wdrożone

`request` (MCP) i `api raw` (CLI) przyjmują `output: original | compact`. Brak wartości oznacza `original`.
Stary parametr `compact: true` (tylko MCP) znaczy „tylko HTML”. Nieznana wartość `output` albo `compact: true`
z `output: original` kończy się błędem przed wysłaniem żądania. Kod: `internal/httpx/compact.go`, `htmltext.go`.

| Dane | Co robi `compact` |
| --- | --- |
| JSON | usuwa tylko białe znaki między tokenami; liczby i napisy kopiowane bajt w bajt, kolejność kluczy bez zmian |
| HTML | zamienia na widoczny tekst (reguły niżej); stratne |
| XML | bez zmian |
| inny tekst | bez zmian |
| binarne | MCP `request` odmawia (jest `download`); CLI `api raw` zapisuje bajty bez zmian |

Ciało to HTML, gdy Content-Type to `text/html` albo zaczyna się od `<!doctype html`, `<html`, `<head`, `<body`.
Poprawny JSON jest minifikowany niezależnie od Content-Type. Ciało zadeklarowane jako JSON, a niepoprawne, oraz HTML
zagnieżdżony głębiej niż 512 poziomów kończą się błędem. Żądanie jest wtedy już wysłane, więc nie powtarzać zapisu.

Reguły HTML:

- zostaje tekst; łamanie wiersza przy elementach blokowych, `|` między komórkami wiersza, układ wewnątrz `<pre>`,
  odstęp między sąsiednimi linkami, przyciskami, etykietami i opcjami, dekodowane encje;
- znika każdy atrybut (cele linków, `alt`, `value`, `placeholder`, wartości ukrytych pól, w tym tokeny CSRF), komentarze,
  `script`, `style`, `head` (z tytułem), `noscript`, `svg`, `template`;
- znika cała zawartość elementu z atrybutem HTML `hidden` (dowolna wartość, także `until-found`);
- zostaje tekst widoczny tylko w kontrolkach (etykieta `option`, treść `textarea`), wartości `input` nie;
- CSS nie jest oceniany: tekst ukryty przez `style` lub klasę zostaje, `aria-hidden="true"` też;
- menu nawigacyjne zostaje w tekście.

Ciała błędów: pierwsze 1200 bajtów, cięte na granicy znaku; HTML pokazany jako ten sam tekst, nigdy jako znaczniki.

PR (`internal/pr/compact.go`): `details` każdego elementu zachowuje stan scalenia, `squash`, `sha`, rozmiar, liczniki
komentarzy i nazwy `labels`, `assignees`, `reviewers`, `requested_reviewers`. Reszta jest usuwana, także pola
spoza listy. Tytuł, opis, stan i gałęzie bez zmian. `format: raw` zwraca `details` dostawcy.

## Wyniki pomiarów

Liczby pochodzą z `compaction.md` i `rtk.md`. To bajty, nie tokeny.

- HTML renderowany po stronie serwera (10–150 KB): typowo 5% oryginału.
- JSON z wcięciami: ok. 20% mniej. JSON już zminifikowany (większość API dostawców): brak zysku.
- RTK 1.2.0, syntetyczne testy Go (bajty JSON `go test` / bajty podsumowania RTK): 41 testów zaliczonych 28151 / 33;
  błąd asercji 1103 / 197; panika 3333 / 376; timeout 5966 / 129.

## Opcjonalny skill RTK (lokalne wyjście poleceń)

RTK filtruje wyjście lokalnych poleceń; workctl obsługuje API dostawców. Żadne nie zależy od drugiego. Plugin ma skill
`compact-output`; RTK nie jest wkompilowany, pobierany ani rejestrowany jako serwer MCP, haki nie są instalowane.
Zmierzone ograniczenie: w podsumowaniu timeoutu brakuje przyczyny; odzyskuje ją `rtk recall HASH --full`. Skill wymaga
odzyskania pominiętych szczegółów błędu. Odpowiedzi workctl nie przechodzą przez RTK.

## Świadomie niezrobione i kryteria podjęcia

| Pomysł | Kiedy zrobić |
| --- | --- |
| (a) tryb dokumentu, który odrzuca nawigację | jest zmierzona luka: menu zostaje w tekście. Zrobić, gdy pomiar na prawdziwych stronach pokaże istotny udział nawigacji; wynik jako osobna wartość `output`, nie zmiana `compact` |
| (b) TOON | dopiero po benchmarku na prawdziwych kształtach odpowiedzi, z testem zachowania dokładności liczb. Biblioteka Go dekoduje liczby do `float64` i zamienia liczby całkowite powyżej 2^53 na napisy, więc round-trip musi być sprawdzony |
| (c) przechowywanie oryginalnej odpowiedzi | przed jakimkolwiek agresywnym obcinaniem: oryginał dostępny przez odczyt stronicowany |
| (d) kompresja XML | tylko dla konkretnego schematu, z listą pól do zachowania |

Poza tym nie ma semantycznego renderera HTML (linki, tabele jako siatki).

## Jak decydować

1. Najpierw pomiar na prawdziwych odpowiedziach, w bajtach i, gdy to możliwe, w tokenach modelu.
2. Jedna nowa opcja naraz.
3. Domyślne zachowanie bez zmian; nowa opcja jest jawna.
4. Każda strata opisana w `compaction.md` w tym samym PR.
5. Zapis nie jest powtarzany po błędzie kompresji.
