package importer

import (
	"reflect"
	"testing"
)

func TestParseSeriesInfo(t *testing.T) {
	tests := []struct {
		filename string
		want     SeriesInfo
		wantOk   bool
	}{
		{
			filename: "South.Park.S27E02.1080p.mkv",
			want:     SeriesInfo{Season: 27, Episodes: []int{2}},
			wantOk:   true,
		},
		{
			filename: "The.Simpsons.S01E01-E02.720p.mkv",
			want:     SeriesInfo{Season: 1, Episodes: []int{1, 2}},
			wantOk:   true,
		},
		{
			filename: "Breaking.Bad.1x05.mp4",
			want:     SeriesInfo{Season: 1, Episodes: []int{5}},
			wantOk:   true,
		},
		{
			filename: "Not.A.Series.Movie.2023.mkv",
			want:     SeriesInfo{},
			wantOk:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got, ok := ParseSeriesInfo(tt.filename)
			if ok != tt.wantOk {
				t.Errorf("ParseSeriesInfo() ok = %v, want %v", ok, tt.wantOk)
			}
			if ok && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseSeriesInfo() = %v, want %v", got, tt.want)
			}
		})
	}
}
