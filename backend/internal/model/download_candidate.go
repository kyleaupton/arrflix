package model

import (
	"errors"
	"strings"
	"time"
)

// DownloadCandidate represents a download candidate from Prowlarr search results
type DownloadCandidate struct {
	Protocol    string    `json:"protocol"`
	Filename    string    `json:"filename"`
	Link        string    `json:"link"`
	Indexer     string    `json:"indexer"`
	IndexerID   int64     `json:"indexerId"`
	GUID        string    `json:"guid"`
	Peers       int       `json:"peers"`   // leechers
	Seeders     int       `json:"seeders"` // seeders
	Age         int64     `json:"age"`     // age in seconds
	AgeHours    float64   `json:"ageHours"`
	Size        int64     `json:"size"` // size in bytes
	Grabs       int       `json:"grabs"`
	Categories   []string  `json:"categories"`
	IndexerFlags []string  `json:"indexerFlags"`
	PublishDate  time.Time `json:"publishDate"`
	Title        string    `json:"title"`
}

func (c *DownloadCandidate) GetMediaType() (MediaType, error) {
	// For now, we'll look for a category that is either "Movies/*", "Movies", "TV/*", or "TV"
	for _, category := range c.Categories {
		if strings.HasPrefix(category, "Movies/") || category == "Movies" {
			return MediaTypeMovie, nil
		}
		if strings.HasPrefix(category, "TV/") || category == "TV" {
			return MediaTypeSeries, nil
		}
	}

	return "", errors.New("no media type found")
}

// EnqueueCandidateRequest is the request body for enqueueing a download candidate
type EnqueueCandidateRequest struct {
	IndexerID int64  `json:"indexerId"`
	GUID      string `json:"guid"`
}
