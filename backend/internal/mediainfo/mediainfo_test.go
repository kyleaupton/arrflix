package mediainfo

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestFormatVideoCodec(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		profile  string
		codecID  string
		expected string
	}{
		{"H.264 AVC", "AVC", "", "", "H.264"},
		{"H.264 h264", "h264", "", "", "H.264"},
		{"H.265 HEVC", "HEVC", "", "", "H.265"},
		{"H.265 h265", "h265", "", "", "H.265"},
		{"AV1", "AV1", "", "", "AV1"},
		{"VP9", "VP9", "", "", "VP9"},
		{"MPEG-2", "MPEG Video", "", "", "MPEG-2"},
		{"Unknown format", "", "", "", "Unknown"},
		{"Xvid", "xvid", "", "", "Xvid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatVideoCodec(tt.format, tt.profile, tt.codecID)
			if result != tt.expected {
				t.Errorf("FormatVideoCodec(%q, %q, %q) = %q, want %q",
					tt.format, tt.profile, tt.codecID, result, tt.expected)
			}
		})
	}
}

func TestFormatAudioCodec(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		profile  string
		codecID  string
		expected string
	}{
		{"AAC", "AAC", "", "", "AAC"},
		{"AC3", "AC-3", "", "", "AC3"},
		{"EAC3", "E-AC-3", "", "", "EAC3"},
		{"DTS-HD MA", "DTS", "MA", "", "DTS-HD MA"},
		{"DTS", "DTS", "", "", "DTS"},
		{"TrueHD", "TrueHD", "", "", "TrueHD"},
		{"FLAC", "FLAC", "", "", "FLAC"},
		{"Opus", "Opus", "", "", "Opus"},
		{"Unknown", "", "", "", "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatAudioCodec(tt.format, tt.profile, tt.codecID)
			if result != tt.expected {
				t.Errorf("FormatAudioCodec(%q, %q, %q) = %q, want %q",
					tt.format, tt.profile, tt.codecID, result, tt.expected)
			}
		})
	}
}

func TestFormatAudioChannels(t *testing.T) {
	tests := []struct {
		name     string
		channels int
		expected string
	}{
		{"Mono", 1, "1.0"},
		{"Stereo", 2, "2.0"},
		{"5.1 Surround", 6, "5.1"},
		{"7.1 Surround", 8, "7.1"},
		{"Unknown", 0, "Unknown"},
		{"Custom 4 channel", 4, "4.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatAudioChannels(tt.channels)
			if result != tt.expected {
				t.Errorf("FormatAudioChannels(%d) = %q, want %q",
					tt.channels, result, tt.expected)
			}
		})
	}
}

func TestFormatHDR(t *testing.T) {
	tests := []struct {
		name     string
		track    *MediaInfoTrack
		expected string
	}{
		{
			name:     "No HDR",
			track:    &MediaInfoTrack{},
			expected: "None",
		},
		{
			name: "Dolby Vision",
			track: &MediaInfoTrack{
				HDRFormat: "Dolby Vision",
			},
			expected: "Dolby Vision",
		},
		{
			name: "HDR10+",
			track: &MediaInfoTrack{
				HDRFormat: "HDR10+",
			},
			expected: "HDR10+",
		},
		{
			name: "HDR10",
			track: &MediaInfoTrack{
				HDRFormat: "HDR10",
			},
			expected: "HDR10",
		},
		{
			name: "HLG from transfer",
			track: &MediaInfoTrack{
				TransferCharacteristics: "HLG",
			},
			expected: "HLG",
		},
		{
			name: "HDR10 from PQ transfer",
			track: &MediaInfoTrack{
				TransferCharacteristics: "SMPTE ST 2084",
			},
			expected: "HDR10",
		},
		{
			name:     "Nil track",
			track:    nil,
			expected: "None",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatHDR(tt.track)
			if result != tt.expected {
				t.Errorf("FormatHDR() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFormatContainer(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		expected string
	}{
		{"Matroska", "matroska", "MKV"},
		{"MP4", "MPEG-4", "MP4"},
		{"AVI", "avi", "AVI"},
		{"TS", "MPEG-TS", "TS"},
		{"WebM", "webm", "WebM"},
		{"Unknown", "", "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatContainer(tt.format)
			if result != tt.expected {
				t.Errorf("FormatContainer(%q) = %q, want %q",
					tt.format, result, tt.expected)
			}
		})
	}
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  int
		shouldErr bool
	}{
		{"Simple integer", "1920", 1920, false},
		{"With spaces", " 1080 ", 1080, false},
		{"With decimal", "23.976", 23, false},
		{"Invalid", "abc", 0, true},
		{"Empty", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseInt(tt.input)
			if tt.shouldErr {
				if err == nil {
					t.Errorf("parseInt(%q) expected error, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("parseInt(%q) unexpected error: %v", tt.input, err)
				}
				if result != tt.expected {
					t.Errorf("parseInt(%q) = %d, want %d", tt.input, result, tt.expected)
				}
			}
		})
	}
}

func TestParseFloat(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  float64
		shouldErr bool
	}{
		{"Simple float", "23.976", 23.976, false},
		{"Integer as float", "30", 30.0, false},
		{"With spaces", " 59.94 ", 59.94, false},
		{"Invalid", "abc", 0, true},
		{"Empty", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseFloat(tt.input)
			if tt.shouldErr {
				if err == nil {
					t.Errorf("parseFloat(%q) expected error, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("parseFloat(%q) unexpected error: %v", tt.input, err)
				}
				if result != tt.expected {
					t.Errorf("parseFloat(%q) = %f, want %f", tt.input, result, tt.expected)
				}
			}
		})
	}
}

func TestExtractFields(t *testing.T) {
	log := zerolog.Nop()
	analyzer := NewAnalyzer(log)

	// Test with a realistic mediainfo response structure
	response := MediaInfoResponse{
		Media: MediaInfoMedia{
			Track: []MediaInfoTrack{
				{
					Type:     "General",
					Format:   "Matroska",
					Duration: "5024.0",
					FileSize: "4294967296",
				},
				{
					Type:      "Video",
					Format:    "AVC",
					Width:     "1920",
					Height:    "1080",
					BitDepth:  "10",
					BitRate:   "8000000",
					FrameRate: "23.976",
					ScanType:  "Progressive",
					HDRFormat: "HDR10",
				},
				{
					Type:     "Audio",
					Format:   "E-AC-3",
					Channels: "6",
					BitRate:  "640000",
					Language: "en",
				},
				{
					Type:     "Audio",
					Format:   "AAC",
					Channels: "2",
					Language: "es",
				},
				{
					Type:     "Text",
					Language: "en",
				},
			},
		},
	}

	fields := analyzer.extractFields(response)

	// Verify general track fields
	if fields.Container != "MKV" {
		t.Errorf("Container = %q, want MKV", fields.Container)
	}
	if fields.Duration != 5024 {
		t.Errorf("Duration = %d, want 5024", fields.Duration)
	}
	if fields.FileSize != 4294967296 {
		t.Errorf("FileSize = %d, want 4294967296", fields.FileSize)
	}

	// Verify video track fields
	if fields.VideoCodec != "H.264" {
		t.Errorf("VideoCodec = %q, want H.264", fields.VideoCodec)
	}
	if fields.Width != 1920 {
		t.Errorf("Width = %d, want 1920", fields.Width)
	}
	if fields.Height != 1080 {
		t.Errorf("Height = %d, want 1080", fields.Height)
	}
	if fields.VideoBitDepth != 10 {
		t.Errorf("VideoBitDepth = %d, want 10", fields.VideoBitDepth)
	}
	if fields.VideoBitrate != 8000000 {
		t.Errorf("VideoBitrate = %d, want 8000000", fields.VideoBitrate)
	}
	if fields.VideoFps != 23.976 {
		t.Errorf("VideoFps = %f, want 23.976", fields.VideoFps)
	}
	if fields.HDR != "HDR10" {
		t.Errorf("HDR = %q, want HDR10", fields.HDR)
	}

	// Verify audio track fields (primary audio)
	if fields.AudioCodec != "EAC3" {
		t.Errorf("AudioCodec = %q, want EAC3", fields.AudioCodec)
	}
	if fields.AudioChannels != "5.1" {
		t.Errorf("AudioChannels = %q, want 5.1", fields.AudioChannels)
	}
	if fields.AudioBitrate != 640000 {
		t.Errorf("AudioBitrate = %d, want 640000", fields.AudioBitrate)
	}
	if fields.AudioStreamCount != 2 {
		t.Errorf("AudioStreamCount = %d, want 2", fields.AudioStreamCount)
	}

	// Verify audio languages
	if len(fields.AudioLanguages) != 2 {
		t.Errorf("AudioLanguages length = %d, want 2", len(fields.AudioLanguages))
	} else {
		if fields.AudioLanguages[0] != "en" {
			t.Errorf("AudioLanguages[0] = %q, want en", fields.AudioLanguages[0])
		}
		if fields.AudioLanguages[1] != "es" {
			t.Errorf("AudioLanguages[1] = %q, want es", fields.AudioLanguages[1])
		}
	}

	// Verify subtitles
	if len(fields.Subtitles) != 1 {
		t.Errorf("Subtitles length = %d, want 1", len(fields.Subtitles))
	} else if fields.Subtitles[0] != "en" {
		t.Errorf("Subtitles[0] = %q, want en", fields.Subtitles[0])
	}
}
