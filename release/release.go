package release

import "time"

type Protocol string

const (
	ProtocolTorrent Protocol = "torrent"
	ProtocolUsenet  Protocol = "usenet"
)

type Release struct {
	Title        string
	Details      string
	DownloadLink string
	InfoHash     string
	Size         int64
	PublishDate  time.Time
	Seeders      int
	Peers        int
	Grabs        int
	Categories   []int
	Protocol     Protocol
	IndexerID    string
	IndexerName  string
}
