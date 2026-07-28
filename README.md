# cardigann-go

`cardigann-go` is a reusable Go implementation of the Cardigann indexer-definition engine.

It provides:

- YAML definition parsing (`definition`)
- definition fetching/sync from tarballs (`defs`)
- the Cardigann search engine (`engine`)
- template rendering and field filters (`template`, `template/filters`)
- selector backends for HTML, JSON, and XML (`selector`)
- per-indexer HTTP client helpers (`httpclient`)
- login flows (`login`)
- a library-local normalized release model (`release`)

## Installation

```bash
go get github.com/Kcchouette/cardigann-go
```

Requires Go 1.23+.

## Usage

```go
package main

import (
    "fmt"
    "log"

    "github.com/Kcchouette/cardigann-go/definition"
    "github.com/Kcchouette/cardigann-go/engine"
)

func main() {
    // Parse a Cardigann YAML definition
    def, err := definition.Parse(yamlBytes)
    if err != nil {
        log.Fatal(err)
    }

    // Create a search provider
    provider := engine.NewProvider(def, "https://example.tracker.com", nil)

    // Execute a search
    releases, err := provider.Search(ctx, engine.Query{Keywords: "1080p"})
    if err != nil {
        log.Fatal(err)
    }

    for _, r := range releases {
        fmt.Printf("%s (%d bytes)\n", r.Title, r.Size)
    }
}
```

## Supported features

| Feature | Status |
|---------|--------|
| HTML scraping (CSS selectors) | Done |
| JSON API (gjson paths) | Done |
| XML/RSS/Newznab (etree) | Done |
| Login: form, cookie, get | Done |
| 21 field filters | Done |
| Row-level filters (andmatch) | Done |
| Template rendering | Done |
| HTTP client (rate limit, retries, proxy) | Done |

## Query Parameters

The following keys are available via `{{ index .Query "<key>" }}` in search paths
and field templates. `Keywords` is also available as `{{ .Keywords }}` at the
top level of the template context.

| Key | Type | Description |
|-----|------|-------------|
| `Keywords` | string | Search keywords (also: `{{ .Keywords }}`) |
| `q` | string | Alias for Keywords |
| `IMDBID` | string | Full IMDb ID, e.g. `tt1234567` |
| `IMDBIDShort` | string | Numeric-only IMDb ID, e.g. `1234567` |
| `TMDBID` | string | The Movie Database ID |
| `TVDBID` | string | TheTVDB series ID |
| `DoubanID` | string | Douban movie ID (Asian indexers) |
| `Season` | string | Season number |
| `Ep` | string | Episode number |
| `Year` | string | Release year |
| `Genre` | string | Genre filter string |
| `categories` | string | Comma-separated native category IDs (when categories are requested) |

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Commit your changes (`git commit -am 'Add my feature'`)
4. Push to the branch (`git push origin feat/my-feature`)
5. Open a Pull Request

## License

This library is licensed under **LGPL-3.0**. You may use/link it from other applications, including closed-source ones, but if you distribute a modified version of this library itself, those modifications must remain available under LGPL-3.0.

## Legal / project notice

- Not affiliated with Prowlarr, Servarr, or the wider `*arr` ecosystem.
- This repository does **not** vendor or redistribute Cardigann indexer definitions or `schema.json`.
- Users are responsible for the definitions and indexers they choose to fetch or use.
