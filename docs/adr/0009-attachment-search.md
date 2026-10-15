# ADR 0009: Search inside attached files (Tika + Tesseract OCR)

Status: Accepted — 2026-10-06

## Context

Pages can carry attached files since 2026-10-05 (PDF, Office, zip,
anything up to 25 MB). The team needs to find things by what is *in*
those files, not just by the page around them: runbooks kept as Word
documents, vendor PDFs, contracts that only exist as scans, screenshots
of error messages. Scanned documents and photos must be searchable too,
so OCR is required, in Hungarian and English.

One rule is not negotiable: search must never show anything from a group
or space the user is not a member of, attachment text and OCR output
included. A restricted group someone keeps for private notes stays
private, also from instance admins who are not in it.

## Decision

**Apache Tika (with Tesseract) as an internal sidecar container; the text
lands in SQLite next to the existing page index (ADR 0007) and is
searched with the same SQL access filter.**

### Extraction

- `deploy/tika/`: a custom image on `apache/tika:4.1.0-full` plus
  `tesseract-ocr-hun`. The official image has no Hungarian.
- Tika 4 ignores the per-request `X-Tika-OCRLanguage` header; the
  language is set in the server config (`tika-config.json`,
  `tesseract-ocr-parser` → `hun+eng`). Hungarian alone misreads Latin
  identifiers (`srvdb01` → `srvdbO1`); `hun+eng` reads both correctly.
- Nook calls `PUT /rmeta/xml`: one JSON object per document (the file
  and every embedded one: zip entries, e-mail attachments), content as
  XHTML that keeps PDF page boundaries (`<div class="page">`). A PDF is
  stored one chunk per page, so a hit can say "p. 12". Scanned pages are
  OCRed automatically (Tika's `auto` PDF strategy).
- The container publishes no port and holds no data; only Nook reaches
  it, over the compose network. `mem_limit: 2g`. Measured: about 550 MB
  after OCR work.

### Queue (`internal/extract`)

- `file_extractions` (migration 0021): one row per file, created by a
  trigger on `page_attachments` insert, so no upload path can forget it.
  States: pending → processing → done | failed | skipped.
- One worker goroutine, **one file at a time**: OCR is CPU-heavy and
  must not compete with the app. It is woken right after an upload and
  also polls every 30 s (retries coming due).
- The Tika call runs outside any transaction: Nook has one SQLite
  connection and a minutes-long OCR must never hold it.
- Errors: Tika down → retried after 1 min, 10 min, 1 h, then failed.
  A file that runs past the time limit fails at once (another try would
  burn the same time again). Unparseable file → failed at once. Password protected →
  skipped. A file left in "processing" by a restart is put back first.
- Limits: 60 min per file (`NOOK_EXTRACT_TIMEOUT`), 5 MB of text kept
  per file, 64 MB maximum Tika response.
- Without `NOOK_TIKA_URL` the worker does not start: the queue waits,
  files are still found by name.

### Index (`file_text`, `file_search_index`)

- `file_text`: chunks (file, PDF page or none, embedded part, OCR flag,
  text). `file_search_index`: FTS5 over (file name, text), same
  `unicode61 remove_diacritics 2` tokenizer as pages, kept in sync by
  triggers on `file_text`, removed by the `page_attachments` cascade.
- Every file always has at least one chunk (empty until extracted), so
  it is findable by name from the moment of upload.

### Query and access

- `search.Store.Search` runs the page query and a file query, both with
  the same `accessFilter` SQL fragment (mirrors
  `permissions.CanAccessSpace`: direct space membership or membership of
  the space's non-archived group; live page, live space; no admin
  bypass). Access is decided at query time, so removing someone from a
  group takes effect on their very next search. No reindexing, no
  separate permission copy that could fall out of sync. This is the
  main reason for staying on SQLite FTS5 instead of an external search
  engine (Meilisearch, OpenSearch).
- A file only counts while its page still references it
  (`instr(content_json, file_id)`): a block deleted from the page
  without its own Delete leaves the file row behind, and that file must
  not keep turning up. Restoring a page version that has the block
  brings it back.
- Best chunk per file (`row_number()` over bm25), file name weighed 10×
  the text. Up to 3 file hits per page; a page can be a result through
  its files alone and ranks by its best match.
- `GET /api/pages/{id}/attachments/{attachmentId}/index` reports the queue state for
  the file block's menu. Same access check as downloading the file.

### Inside words (migration 0023)

- Word-prefix matching missed hostnames and codes: "db01" did not find
  "srvdb01". Two trigram FTS5 tables now mirror the word indexes row for
  row (`search_trigram`, `file_search_trigram`, tokenizer
  `trigram remove_diacritics 1`); every trigger that writes a word index
  writes its twin too.
- The word index is asked first; the trigram index only adds what that
  missed, ranked after every word hit. Terms of 1-2 characters can't be
  trigram-matched, so in that query they still have to match as word
  prefixes in the word index: every typed term must match, as before.
- Same `accessFilter`, same "file still on its page" rule.
- Cost: the trigram tables are a few times the size of the text they
  index. Measured on the live data: migration 11 ms, a search 2-3 ms.

### UI

- Sidebar search and the Overview box list file hits under their page:
  📎 name, where (part, page, OCR), excerpt. Opening one opens the page
  and the file preview at the matching PDF page (`#page=N`).
- The file block's menu shows: waiting / reading / searchable / OCR /
  no text found / only the name is searchable (with the reason).

## Alternatives considered

- **Meilisearch / Typesense / OpenSearch:** better ranking and typo
  tolerance, but a second copy of the data whose permissions must be kept
  in sync, a sync job, and in OpenSearch's case 2+ GB of JVM. Rejected for
  now; FTS5 keeps access control in one place.
- **Own pipeline (pdftotext + tesseract + LibreOffice):** smaller pieces,
  but much more glue code and fewer formats (e-mail, zip, old Office).
- **Cloud OCR (Azure, Google):** best quality, but documents would leave
  the company, which defeats the reason Nook exists.

## Consequences

- One more container (about 0.6 GB image, about 0.5–1 GB RAM in use).
- The text of every attachment is now in the database, so in the backups
  too. It is no more sensitive than the files themselves, which are
  already backed up.
- OCR quality depends on the scan; misread characters are possible. Hits
  from OCR text are marked so.
- Not done yet: an admin view of failed files with reprocessing,
  Hungarian stemming. OCR of images pasted into the page body (not
  attachments) is deliberately left out for now, as is a text layer in
  scanned PDFs for the browser's own Ctrl+F.
