package model

import "github.com/google/uuid"

// PathCheckStatus is whether Arrflix can see where a downloader saves.
type PathCheckStatus string

const (
	// PathCheckNoMappingNeeded: the downloader's save path exists in Arrflix
	// as-is; no volume applied.
	PathCheckNoMappingNeeded PathCheckStatus = "no_mapping_needed"
	// PathCheckMapped: a volume maps the save path to a directory that exists.
	PathCheckMapped PathCheckStatus = "mapped"
	// PathCheckNotVisible: the resolved save path isn't a directory Arrflix
	// can see, so imports from this downloader will fail.
	PathCheckNotVisible PathCheckStatus = "not_visible"
	// PathCheckUnavailable: the check couldn't run, because the downloader is
	// disabled or didn't report its save path. Message says which.
	PathCheckUnavailable PathCheckStatus = "unavailable"
)

// DownloaderPathCheck is the result of asking one downloader for its default
// save path and looking for it in Arrflix through the path mappings.
type DownloaderPathCheck struct {
	DownloaderID   uuid.UUID       `json:"downloaderId"`
	DownloaderName string          `json:"downloaderName"`
	Status         PathCheckStatus `json:"status" enum:"no_mapping_needed,mapped,not_visible,unavailable"`
	// SavePath is the downloader's default save path, in its own frame.
	SavePath string `json:"savePath,omitempty"`
	// ResolvedPath is SavePath in Arrflix's frame.
	ResolvedPath string     `json:"resolvedPath,omitempty"`
	VolumeID     *uuid.UUID `json:"volumeId,omitempty"`
	VolumeName   string     `json:"volumeName,omitempty"`
	// Message explains an unavailable check.
	Message string `json:"message,omitempty"`
}

// PathTestResult is the outcome of testing one path, as a downloader sees it,
// against the path mappings and every library.
type PathTestResult struct {
	DownloaderID uuid.UUID `json:"downloaderId"`
	// Path is the tested path, in the downloader's frame.
	Path         string     `json:"path"`
	ResolvedPath string     `json:"resolvedPath"`
	VolumeID     *uuid.UUID `json:"volumeId,omitempty"`
	VolumeName   string     `json:"volumeName,omitempty"`
	Exists       bool       `json:"exists"`
	// Libraries holds one verdict per library; empty when the path doesn't exist.
	Libraries []PathTestLibrary `json:"libraries"`
}

// PathTestLibrary is whether imports from a tested path into one library
// hardlink or copy.
type PathTestLibrary struct {
	LibraryID   uuid.UUID `json:"libraryId"`
	LibraryName string    `json:"libraryName"`
	RootPath    string    `json:"rootPath"`
	Verdict     string    `json:"verdict" enum:"hardlink,copy,unknown"`
	// Message explains an unknown verdict.
	Message string `json:"message,omitempty"`
}
