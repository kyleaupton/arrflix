package service

import (
	"encoding/json"
	"testing"

	tmdb "github.com/cyruzin/golang-tmdb"
	"github.com/kyleaupton/arrflix/internal/model"
)

func TestExtractMovieReleaseDates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		json string
		want *model.ReleaseDatesByType
	}{
		{
			name: "nil input",
			json: "",
			want: nil,
		},
		{
			name: "no results",
			json: `{"results":[]}`,
			want: nil,
		},
		{
			name: "us with all three types, timestamps trimmed to date",
			json: `{"results":[{"iso_3166_1":"US","release_dates":[
				{"type":3,"release_date":"2018-12-15T00:00:00.000Z"},
				{"type":4,"release_date":"2019-03-01T00:00:00.000Z"},
				{"type":5,"release_date":"2019-04-02T00:00:00.000Z"}
			]}]}`,
			want: &model.ReleaseDatesByType{
				Theatrical: "2018-12-15",
				Digital:    "2019-03-01",
				Physical:   "2019-04-02",
			},
		},
		{
			name: "ignores premiere/limited/tv types",
			json: `{"results":[{"iso_3166_1":"US","release_dates":[
				{"type":1,"release_date":"2018-11-01T00:00:00.000Z"},
				{"type":2,"release_date":"2018-11-10T00:00:00.000Z"},
				{"type":3,"release_date":"2018-12-15T00:00:00.000Z"},
				{"type":6,"release_date":"2020-01-01T00:00:00.000Z"}
			]}]}`,
			want: &model.ReleaseDatesByType{Theatrical: "2018-12-15"},
		},
		{
			name: "region priority: us wins over gb",
			json: `{"results":[
				{"iso_3166_1":"GB","release_dates":[{"type":3,"release_date":"2018-12-20T00:00:00.000Z"}]},
				{"iso_3166_1":"US","release_dates":[{"type":3,"release_date":"2018-12-15T00:00:00.000Z"}]}
			]}`,
			want: &model.ReleaseDatesByType{Theatrical: "2018-12-15"},
		},
		{
			name: "region fallback: gb when no us",
			json: `{"results":[
				{"iso_3166_1":"FR","release_dates":[{"type":3,"release_date":"2018-12-05T00:00:00.000Z"}]},
				{"iso_3166_1":"GB","release_dates":[{"type":4,"release_date":"2019-02-01T00:00:00.000Z"}]}
			]}`,
			want: &model.ReleaseDatesByType{Digital: "2019-02-01"},
		},
		{
			name: "internally consistent: does not mix regions",
			json: `{"results":[
				{"iso_3166_1":"US","release_dates":[{"type":3,"release_date":"2018-12-15T00:00:00.000Z"}]},
				{"iso_3166_1":"GB","release_dates":[{"type":4,"release_date":"2019-02-01T00:00:00.000Z"}]}
			]}`,
			want: &model.ReleaseDatesByType{Theatrical: "2018-12-15"},
		},
		{
			name: "priority region present but only non-mapped types falls through",
			json: `{"results":[
				{"iso_3166_1":"US","release_dates":[{"type":1,"release_date":"2018-11-01T00:00:00.000Z"}]},
				{"iso_3166_1":"GB","release_dates":[{"type":3,"release_date":"2018-12-20T00:00:00.000Z"}]}
			]}`,
			want: &model.ReleaseDatesByType{Theatrical: "2018-12-20"},
		},
		{
			name: "no priority region present",
			json: `{"results":[{"iso_3166_1":"FR","release_dates":[{"type":3,"release_date":"2018-12-05T00:00:00.000Z"}]}]}`,
			want: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var rd *tmdb.MovieReleaseDates
			if tt.json != "" {
				rd = &tmdb.MovieReleaseDates{}
				if err := json.Unmarshal([]byte(tt.json), rd); err != nil {
					t.Fatalf("unmarshal fixture: %v", err)
				}
			}

			got := extractMovieReleaseDates(rd)

			switch {
			case tt.want == nil && got != nil:
				t.Fatalf("want nil, got %+v", *got)
			case tt.want != nil && got == nil:
				t.Fatalf("want %+v, got nil", *tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Fatalf("want %+v, got %+v", *tt.want, *got)
			}
		})
	}
}
