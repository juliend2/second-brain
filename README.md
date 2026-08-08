# data-agent

Aggregator and repository for a personal "second brain", built on top of the
existing [personal-search-engine](personal-search-engine/).

## Concept

Everything becomes a **node**: `(source, id, markdown, metadata, timestamps)`.
Nodes are mirrored as markdown into a local, git-versioned store (the working
copy — the web is the source of truth). Relationships between nodes form a
graph. The LLM enriches nodes automatically.

```
ingest → store → enrich → serve
```

## Sources

- **Notion** — via the existing personal-search-engine crawler (API → markdown).
- **Dropbox** — via the official Dropbox client, which syncs to a local folder
  (`~/Dropbox`); we watch it with `fsnotify` instead of polling the API.

Mobile input does **not** go through a capture inbox: all input from mobile goes
through the Notion mobile app.

## Data model

- Markdown store: git-versioned folder of node markdown files.
- SQLite: metadata, edges, embeddings (`sqlite-vec`), status.
- Bleve: full-text search index, rebuilt from the store.

## Enrichment (hybrid)

- **Local embeddings** — Ollama + `nomic-embed-text` on the host machine. Keeps
  indexing/retrieval and dedup on the machine.
- **Cloud LLM** — used only for semantic work, one call per new node:
  - tags
  - `[[wikilinks]]` candidates, written inline in the markdown (portable to
    Obsidian, parseable back into the edge table)
  - one-line summary
  - **"connects to"** edges — labeled edges with a short reason explaining
    *why* two nodes connect; surfaced in the UI.
- Auto-applied by default; audit occasionally.

## Serving

One HTTP API (`/api/nodes`, `/api/search`, `/api/graph`) behind three clients:

- **MCP server** — thin wrapper around the API exposing tools to LLM clients
  (opencode today, others later).
- **Mobile PWA** — served over HTTPS via a Tailscale tailnet certificate
  (`machine.tailnet.ts.net`); mobile-first, installable. Main feature is
  **read-with-related**: show a node's markdown plus links in/out and nearest
  neighbors via embeddings.

No CLI (dropped — interactions happen via MCP and the PWA).

## Hosting

ThinkCentre, ~4GB RAM, on the tailnet. Budget RAM carefully: Ollama +
nomic-embed-text (~1–2GB) + Go service + bleve + SQLite must all fit.

## TODO

- [ ] Cross-cluster pass: cluster nodes by similarity, ask the LLM to propose
      synthesis/idea notes that bridge clusters ("forming new ideas and
      intuitions"). Nice-to-have, deferred.
- [ ] Ingest: Notion crawler
- [ ] Ingest: Dropbox fsnotify watcher
- [x] Store: canonical markdown mirror + SQLite metadata + node API
      (`internal/store`)
- [ ] Enrich: local embeddings (Ollama + nomic-embed-text)
- [ ] Enrich: cloud pass (tags / wikilinks / summary / connects-to)
- [ ] Serve: HTTP API
- [ ] Serve: MCP server
- [ ] Serve: mobile PWA (read-with-related)
