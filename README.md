# cardigann-go

`cardigann-go` is a reusable Go implementation of the Cardigann indexer-definition engine.

It provides:

- YAML definition parsing (`definition`)
- definition fetching/sync from tarballs (`defs`)
- the Cardigann search engine (`engine`)
- template rendering and field filters (`template`, `template/filters`)
- selector backends for HTML and JSON (`selector`)
- per-indexer HTTP client helpers (`httpclient`)
- login flows (`login`)
- a library-local normalized release model (`release`)

## License

This library is licensed under **LGPL-3.0**. You may use/link it from other applications, including closed-source ones, but if you distribute a modified version of this library itself, those modifications must remain available under LGPL-3.0.

## Legal / project notice

- Not affiliated with Prowlarr, Servarr, or the wider `*arr` ecosystem.
- This repository does **not** vendor or redistribute Cardigann indexer definitions or `schema.json`.
- Users are responsible for the definitions and indexers they choose to fetch or use.
