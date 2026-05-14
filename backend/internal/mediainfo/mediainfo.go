// Package mediainfo provides video file analysis using the mediainfo CLI binary.
// This package extracts detailed metadata from media files that is not available
// from release titles alone (e.g., actual video codec, audio tracks, HDR info).
package mediainfo

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/rs/zerolog"
)

// Analyzer extracts technical metadata from video files using mediainfo CLI.
type Analyzer struct {
	// mediaInfoPath is the path to the mediainfo binary
	mediaInfoPath string
	// timeout for mediainfo execution
	timeout time.Duration
	// logger for error reporting
	log zerolog.Logger
}

// NewAnalyzer creates a new media analyzer.
// By default, it looks for mediainfo in the PATH.
func NewAnalyzer(log zerolog.Logger) *Analyzer {
	return &Analyzer{
		mediaInfoPath: "mediainfo",
		timeout:       30 * time.Second,
		log:           log,
	}
}

// WithMediaInfoPath sets a custom path to the mediainfo binary.
func (a *Analyzer) WithMediaInfoPath(path string) *Analyzer {
	a.mediaInfoPath = path
	return a
}

// WithTimeout sets a custom timeout for mediainfo execution.
func (a *Analyzer) WithTimeout(timeout time.Duration) *Analyzer {
	a.timeout = timeout
	return a
}

// MediaInfoResponse represents the JSON structure returned by mediainfo --Output=JSON
type MediaInfoResponse struct {
	Media MediaInfoMedia `json:"media"`
}

type MediaInfoMedia struct {
	Track []MediaInfoTrack `json:"track"`
}

type MediaInfoTrack struct {
	Type                           string `json:"@type"`
	Format                         string `json:"Format"`
	FormatProfile                  string `json:"Format_Profile"`
	CodecID                        string `json:"CodecID"`
	Duration                       string `json:"Duration"`
	BitRate                        string `json:"BitRate"`
	Width                          string `json:"Width"`
	Height                         string `json:"Height"`
	BitDepth                       string `json:"BitDepth"`
	FrameRate                      string `json:"FrameRate"`
	ScanType                       string `json:"ScanType"`
	ColorPrimaries                 string `json:"colour_primaries"`
	TransferCharacteristics        string `json:"transfer_characteristics"`
	Channels                       string `json:"Channels"`
	ChannelLayout                  string `json:"ChannelLayout"`
	ChannelPositions               string `json:"ChannelPositions"`
	Language                       string `json:"Language"`
	FileSize                       string `json:"FileSize"`
	StreamCount                    string `json:"StreamCount"`
	AudioCount                     string `json:"AudioCount"`
	TextCount                      string `json:"TextCount"`
	MultiViewCount                 string `json:"MultiView_Count"`
	HDRFormat                      string `json:"HDR_Format"`
	HDRFormatCompatibility         string `json:"HDR_Format_Compatibility"`
	MasteringDisplayColorPrimaries string `json:"MasteringDisplay_ColorPrimaries"`
}

// Analyze extracts technical metadata from a video file.
// Returns nil if the file cannot be analyzed (with error logged).
func (a *Analyzer) Analyze(filePath string) *model.MediaInfoFields {
	ctx, cancel := context.WithTimeout(context.Background(), a.timeout)
	defer cancel()

	// Execute mediainfo with JSON output
	cmd := exec.CommandContext(ctx, a.mediaInfoPath, "--Output=JSON", filePath)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			a.log.Warn().Str("path", filePath).Msg("mediainfo timed out")
		} else {
			a.log.Warn().Err(err).Str("path", filePath).Msg("mediainfo execution failed")
		}
		return nil
	}

	// Parse JSON response
	var response MediaInfoResponse
	if err := json.Unmarshal(output, &response); err != nil {
		a.log.Warn().Err(err).Str("path", filePath).Msg("failed to parse mediainfo JSON")
		return nil
	}

	// Extract and map fields
	fields := a.extractFields(response)
	return fields
}

// extractFields maps mediainfo JSON response to our MediaInfoFields struct
func (a *Analyzer) extractFields(response MediaInfoResponse) *model.MediaInfoFields {
	fields := &model.MediaInfoFields{
		VideoCodec:          "Unknown",
		AudioCodec:          "Unknown",
		AudioChannels:       "Unknown",
		Container:           "Unknown",
		HDR:                 "None",
		ScanType:            "Unknown",
		VideoMultiViewCount: 1,
	}

	var generalTrack *MediaInfoTrack
	var videoTrack *MediaInfoTrack
	var audioTracks []MediaInfoTrack
	var textTracks []MediaInfoTrack

	// Separate tracks by type
	for i := range response.Media.Track {
		track := &response.Media.Track[i]
		switch track.Type {
		case "General":
			generalTrack = track
		case "Video":
			if videoTrack == nil {
				videoTrack = track
			}
		case "Audio":
			audioTracks = append(audioTracks, *track)
		case "Text":
			textTracks = append(textTracks, *track)
		}
	}

	// Extract general track info
	if generalTrack != nil {
		if generalTrack.Format != "" {
			fields.Container = FormatContainer(generalTrack.Format)
		}
		if generalTrack.Duration != "" {
			if duration, err := parseFloat(generalTrack.Duration); err == nil {
				fields.Duration = int64(duration)
			}
		}
		if generalTrack.FileSize != "" {
			if size, err := parseInt64(generalTrack.FileSize); err == nil {
				fields.FileSize = size
			}
		}
	}

	// Extract video track info
	if videoTrack != nil {
		fields.VideoCodec = FormatVideoCodec(videoTrack.Format, videoTrack.FormatProfile, videoTrack.CodecID)

		if videoTrack.Width != "" {
			if width, err := parseInt(videoTrack.Width); err == nil {
				fields.Width = width
			}
		}
		if videoTrack.Height != "" {
			if height, err := parseInt(videoTrack.Height); err == nil {
				fields.Height = height
			}
		}
		if videoTrack.BitDepth != "" {
			if bitDepth, err := parseInt(videoTrack.BitDepth); err == nil {
				fields.VideoBitDepth = bitDepth
			}
		}
		if videoTrack.BitRate != "" {
			if bitrate, err := parseInt64(videoTrack.BitRate); err == nil {
				fields.VideoBitrate = bitrate
			}
		}
		if videoTrack.FrameRate != "" {
			if fps, err := parseFloat(videoTrack.FrameRate); err == nil {
				fields.VideoFps = fps
			}
		}
		if videoTrack.FormatProfile != "" {
			fields.VideoProfile = videoTrack.FormatProfile
		}
		if videoTrack.ScanType != "" {
			fields.ScanType = videoTrack.ScanType
		} else {
			fields.ScanType = "Progressive"
		}
		if videoTrack.MultiViewCount != "" {
			if mvc, err := parseInt(videoTrack.MultiViewCount); err == nil {
				fields.VideoMultiViewCount = mvc
			}
		}

		// Detect HDR
		fields.HDR = FormatHDR(videoTrack)
	}

	// Extract audio track info (primary audio track)
	if len(audioTracks) > 0 {
		primaryAudio := audioTracks[0]
		fields.AudioCodec = FormatAudioCodec(primaryAudio.Format, primaryAudio.FormatProfile, primaryAudio.CodecID)
		fields.AudioStreamCount = len(audioTracks)

		if primaryAudio.Channels != "" {
			if channels, err := parseInt(primaryAudio.Channels); err == nil {
				fields.AudioChannels = FormatAudioChannels(channels)
			}
		}
		if primaryAudio.BitRate != "" {
			if bitrate, err := parseInt64(primaryAudio.BitRate); err == nil {
				fields.AudioBitrate = bitrate
			}
		}
		if primaryAudio.FormatProfile != "" {
			fields.AudioProfile = primaryAudio.FormatProfile
		}

		// Collect audio languages
		var languages []string
		for _, track := range audioTracks {
			if track.Language != "" && track.Language != "und" {
				languages = append(languages, track.Language)
			}
		}
		if len(languages) > 0 {
			fields.AudioLanguages = languages
		}
	}

	// Extract subtitle languages
	if len(textTracks) > 0 {
		var subtitles []string
		for _, track := range textTracks {
			if track.Language != "" && track.Language != "und" {
				subtitles = append(subtitles, track.Language)
			}
		}
		if len(subtitles) > 0 {
			fields.Subtitles = subtitles
		}
	}

	return fields
}

// Helper functions for parsing

func parseInt(s string) (int, error) {
	// Remove any spaces and handle decimal values (take integer part)
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, "."); idx != -1 {
		s = s[:idx]
	}
	return strconv.Atoi(s)
}

func parseInt64(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, "."); idx != -1 {
		s = s[:idx]
	}
	return strconv.ParseInt(s, 10, 64)
}

func parseFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	return strconv.ParseFloat(s, 64)
}

// FormatVideoCodec maps raw video codec names to friendly names
func FormatVideoCodec(format, _, codecID string) string {
	format = strings.ToLower(strings.TrimSpace(format))

	switch {
	case strings.Contains(format, "avc") || format == "h264" || strings.Contains(codecID, "avc"):
		return "H.264"
	case strings.Contains(format, "hevc") || format == "h265" || strings.Contains(codecID, "hev") || strings.Contains(codecID, "hvc"):
		return "H.265"
	case format == "av1" || strings.Contains(format, "av1"):
		return "AV1"
	case format == "vp9":
		return "VP9"
	case format == "vp8":
		return "VP8"
	case strings.Contains(format, "mpeg") && (strings.Contains(format, "video") || strings.Contains(format, "-2")):
		return "MPEG-2"
	case strings.Contains(format, "mpeg") && strings.Contains(format, "4"):
		return "MPEG-4"
	case format == "vc-1" || strings.Contains(format, "vc1"):
		return "VC-1"
	case format == "xvid":
		return "Xvid"
	case format == "divx":
		return "DivX"
	default:
		if format != "" {
			return strings.ToUpper(format[:1]) + format[1:]
		}
		return "Unknown"
	}
}

// FormatAudioCodec maps raw audio codec names to friendly names
func FormatAudioCodec(format, profile, codecID string) string {
	format = strings.ToLower(strings.TrimSpace(format))
	profile = strings.ToLower(strings.TrimSpace(profile))

	switch {
	case format == "aac" || strings.Contains(format, "aac"):
		return "AAC"
	case format == "e-ac-3" || format == "eac3" || strings.Contains(format, "e-ac-3"):
		return "EAC3"
	case format == "ac-3" || format == "ac3" || strings.Contains(format, "ac-3"):
		return "AC3"
	case strings.Contains(format, "dts") && (strings.Contains(profile, "ma") || strings.Contains(format, "ma")):
		return "DTS-HD MA"
	case strings.Contains(format, "dts") && (strings.Contains(profile, "hra") || strings.Contains(format, "hra")):
		return "DTS-HD HRA"
	case strings.Contains(format, "dts") && strings.Contains(format, "x"):
		return "DTS:X"
	case strings.Contains(format, "dts"):
		return "DTS"
	case strings.Contains(format, "truehd") || strings.Contains(format, "mlp fba"):
		return "TrueHD"
	case strings.Contains(format, "atmos"):
		return "Atmos"
	case format == "flac":
		return "FLAC"
	case format == "opus":
		return "Opus"
	case format == "vorbis":
		return "Vorbis"
	case strings.Contains(format, "mp3") || format == "mpeg audio":
		return "MP3"
	case strings.Contains(format, "pcm"):
		return "PCM"
	default:
		if format != "" {
			return strings.ToUpper(format[:1]) + format[1:]
		}
		return "Unknown"
	}
}

// FormatAudioChannels converts channel count to standard format
func FormatAudioChannels(channels int) string {
	switch channels {
	case 1:
		return "1.0"
	case 2:
		return "2.0"
	case 6:
		return "5.1"
	case 8:
		return "7.1"
	default:
		if channels > 0 {
			return fmt.Sprintf("%d.0", channels)
		}
		return "Unknown"
	}
}

// FormatHDR detects and formats HDR information
func FormatHDR(track *MediaInfoTrack) string {
	if track == nil {
		return "None"
	}

	hdrFormat := strings.ToLower(strings.TrimSpace(track.HDRFormat))
	hdrCompat := strings.ToLower(strings.TrimSpace(track.HDRFormatCompatibility))
	transfer := strings.ToLower(strings.TrimSpace(track.TransferCharacteristics))

	// Dolby Vision detection
	if strings.Contains(hdrFormat, "dolby") || strings.Contains(hdrCompat, "dolby") {
		return "Dolby Vision"
	}

	// HDR10+ detection
	if strings.Contains(hdrFormat, "hdr10+") || strings.Contains(hdrCompat, "hdr10+") {
		return "HDR10+"
	}

	// HDR10 detection
	if strings.Contains(hdrFormat, "hdr10") || strings.Contains(hdrCompat, "hdr10") {
		return "HDR10"
	}

	// HLG detection
	if strings.Contains(hdrFormat, "hlg") || strings.Contains(transfer, "hlg") || strings.Contains(transfer, "arib") {
		return "HLG"
	}

	// PQ transfer function indicates HDR10
	if strings.Contains(transfer, "smpte st 2084") || strings.Contains(transfer, "pq") {
		return "HDR10"
	}

	return "None"
}

// FormatContainer normalizes container format names
func FormatContainer(format string) string {
	format = strings.ToLower(strings.TrimSpace(format))

	switch format {
	case "matroska":
		return "MKV"
	case "mpeg-4", "isom":
		return "MP4"
	case "avi":
		return "AVI"
	case "mpeg-ts", "bdav":
		return "TS"
	case "webm":
		return "WebM"
	default:
		if format != "" {
			return strings.ToUpper(format[:1]) + format[1:]
		}
		return "Unknown"
	}
}
