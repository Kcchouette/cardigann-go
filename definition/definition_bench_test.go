package definition

import "testing"

// benchDefinitionYAML is a realistic Cardigann v11 definition (~1.5 KB)
// shaped like the Prowlarr corpus: settings, caps, login form, search path,
// row filters, fields with filter chains, and a download block.
const benchDefinitionYAML = `
id: benchtracker
name: BenchTracker
description: Benchmark fixture definition with realistic structure
language: en-US
type: semi-private
encoding: UTF-8
links:
  - https://example.invalid/
legacylinks:
  - https://legacy.example.invalid/
requestDelay: 2
caps:
  categorymappings:
    - id: "100"
      cat: "2000"
      desc: Movies
    - id: "200"
      cat: "2010"
      desc: Movies/Foreign
    - id: "300"
      cat: "5000"
      desc: Audio
  modes:
    search: ["q"]
settings:
  - name: username
    type: text
    label: Username
  - name: password
    type: password
    label: Password
  - name: freeleech
    type: checkbox
    label: Freeleech only
    default: "false"
login:
  method: form
  path: /login
  inputs:
    username: "{{ .Config.username }}"
    password: "{{ .Config.password }}"
  selectorinputs:
    csrf_token:
      selector: input[name=csrf_token]
      attribute: value
  test:
    path: /check
    selector: div.logged-in
search:
  paths:
    - path: /search
      method: get
      inputs:
        q: "{{ .Keywords }}"
        category: "{{ .Query.categories }}"
      response:
        type: html
  rows:
    selector: table.results tr
    filters:
      - name: andmatch
  headers:
    Authorization: ["Bearer {{ .Config.apikey }}"]
  fields:
    title:
      selector: a.title
      filters:
        - name: htmldecode
        - name: trim
    details:
      selector: a.title
      attribute: href
    download:
      selector: a.dl
      attribute: href
      filters:
        - name: urlencode
    size:
      selector: td.size
      filters:
        - name: replace
          args: [" ", ""]
    seeders:
      selector: td.seeders
      filters:
        - name: regexp
          args: ["(\\d+)"]
    date:
      selector: td.date
      filters:
        - name: dateparse
download:
  before:
    path: /api/info
  selectors:
    - selector: a.dl
      attribute: href
      filters:
        - name: querystring
          args: ["link"]
  infohash:
    usebeforeresponse: true
    hash:
      selector: "[name=hash]"
      filters:
        - name: regexp
          args: ["([a-f0-9]{40})"]
`

// BenchmarkParse_DefinitionYAML measures the full YAML decode of a Cardigann
// definition (no cache at this layer — callers decide).
func BenchmarkParse_DefinitionYAML(b *testing.B) {
	data := []byte(benchDefinitionYAML)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Parse(data); err != nil {
			b.Fatal(err)
		}
	}
}
