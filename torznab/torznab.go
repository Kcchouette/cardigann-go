// Package torznab generates Torznab-compatible XML responses
// (https://torznab.github.io/schemas/2015/feed) for interoperability
// with clients like Sonarr/Radarr/Prowlarr.
package torznab

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Kcchouette/cardigann-go/release"
)

const (
	// Namespace is the official Torznab XML namespace.
	Namespace = "http://torznab.github.io/schemas/2015/feed"
	// AtomNamespace is the Atom feed namespace used alongside Torznab.
	AtomNamespace = "http://www.w3.org/2005/Atom"
)

// Response is the root RSS element with Torznab xmlns attributes.
type Response struct {
	XMLName      xml.Name `xml:"rss"`
	Version      string   `xml:"version,attr"`
	XmlnsAtom    string   `xml:"xmlns:atom,attr"`
	XmlnsTorznab string   `xml:"xmlns:torznab,attr"`
	Channel      Channel  `xml:"channel"`
}

// Channel contains the feed metadata and items.
type Channel struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	Items       []Item `xml:"item"`
}

// Item represents a single release in the Torznab feed.
type Item struct {
	Title      string        `xml:"title"`
	Link       string        `xml:"link"`
	Comments   string        `xml:"comments,omitempty"`
	PubDate    string        `xml:"pubDate,omitempty"`
	Guid       string        `xml:"guid"`
	Enclosure  *Enclosure    `xml:"enclosure,omitempty"`
	Size       int64         `xml:"size"`
	Categories []string      `xml:"category"`
	Attrs      []TorznabAttr `xml:"attr"`
}

// Enclosure represents the download enclosure.
type Enclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

// TorznabAttr is a torznab:attr element for extended metadata.
type TorznabAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// WriteReleases writes a Torznab-compatible XML feed to w.
func WriteReleases(w io.Writer, releases []release.Release, indexerName string) error {
	channel := Channel{
		Title:       indexerName,
		Link:        indexerName,
		Description: fmt.Sprintf("Torznab feed for %s", indexerName),
	}

	items := make([]Item, 0, len(releases))
	for _, r := range releases {
		item := releaseToItem(r)
		items = append(items, item)
	}
	channel.Items = items

	resp := Response{
		Version:      "2.0",
		XmlnsAtom:    AtomNamespace,
		XmlnsTorznab: Namespace,
		Channel:      channel,
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return fmt.Errorf("writing xml header: %w", err)
	}

	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(resp); err != nil {
		return fmt.Errorf("encoding torznab response: %w", err)
	}
	return nil
}

// releaseGUID derives a unique Torznab GUID for r. Using r.Title alone
// (previous behavior) causes collisions when two releases share the same
// title (frequent reposts/duplicates on trackers): Torznab clients
// (Sonarr/Radarr/NZBHydra) deduplicate on GUID and would silently drop one
// of the two legitimate results.
//
// InfoHash, when available, is already a stable and unique identifier per
// torrent: we prefer it. Otherwise, we derive a hash from the triplet
// Title+DownloadLink+Details, which distinguishes reposts of the same title
// as long as they have a different download link or details.
func releaseGUID(r release.Release) string {
	if r.InfoHash != "" {
		return r.InfoHash
	}
	sum := sha256.Sum256([]byte(r.Title + "|" + r.DownloadLink + "|" + r.Details))
	return hex.EncodeToString(sum[:])
}

// releaseToItem converts a release.Release to a Torznab Item.
func releaseToItem(r release.Release) Item {
	item := Item{
		Title: r.Title,
		Guid:  releaseGUID(r),
		Size:  r.Size,
	}

	if r.DownloadLink != "" {
		item.Link = r.DownloadLink
		item.Enclosure = &Enclosure{
			URL:    r.DownloadLink,
			Length: r.Size,
			Type:   "application/x-bittorrent",
		}
	}

	if r.Details != "" {
		item.Comments = r.Details
	}

	if !r.PublishDate.IsZero() {
		item.PubDate = r.PublishDate.Format("Mon, 02 Jan 2006 15:04:05 -0700")
	}

	// Map release categories (integers) to strings for XML.
	for _, cat := range r.Categories {
		item.Categories = append(item.Categories, strconv.Itoa(cat))
	}

	// Build torznab:attr elements for extended attributes.
	item.Attrs = buildTorznabAttrs(r)

	return item
}

// buildTorznabAttrs constructs torznab:attr elements from release fields.
func buildTorznabAttrs(r release.Release) []TorznabAttr {
	var attrs []TorznabAttr

	if r.Seeders > 0 {
		attrs = append(attrs, TorznabAttr{Name: "seeders", Value: strconv.Itoa(r.Seeders)})
	}
	if r.Peers > 0 {
		attrs = append(attrs, TorznabAttr{Name: "peers", Value: strconv.Itoa(r.Peers)})
	}
	if r.Grabs > 0 {
		attrs = append(attrs, TorznabAttr{Name: "grabs", Value: strconv.Itoa(r.Grabs)})
	}
	if r.InfoHash != "" {
		attrs = append(attrs, TorznabAttr{Name: "infohash", Value: r.InfoHash})
	}
	if r.IndexerID != "" {
		attrs = append(attrs, TorznabAttr{Name: "indexer", Value: r.IndexerID})
	}

	// Map Newznab category IDs to torznab:attr.
	for _, cat := range r.Categories {
		attrs = append(attrs, TorznabAttr{
			Name:  "category",
			Value: strconv.Itoa(cat),
		})
	}

	return attrs
}

// WriteReleasesString is a convenience wrapper that returns the XML as a string.
func WriteReleasesString(releases []release.Release, indexerName string) (string, error) {
	var buf strings.Builder
	if err := WriteReleases(&buf, releases, indexerName); err != nil {
		return "", err
	}
	return buf.String(), nil
}
