export interface TemplatePreset {
  id: string
  label: string
  description: string
  movie: {
    template: string
    movieDirTemplate: string
  }
  series: {
    template: string
    seriesShowTemplate: string
    seriesSeasonTemplate: string
  }
}

export const presets: TemplatePreset[] = [
  {
    id: 'simple',
    label: 'Simple',
    description: 'Clean and minimal naming with resolution only',
    movie: {
      movieDirTemplate: '{{.Media.CleanTitle}} ({{.Media.Year}})',
      template: '{{.Media.CleanTitle}} ({{.Media.Year}}) [{{.Quality.Resolution}}]',
    },
    series: {
      seriesShowTemplate: '{{.Media.Title}} ({{.Media.Year}})',
      seriesSeasonTemplate: 'Season {{.Media.Season}}',
      template:
        '{{.Media.Title}} - S{{.Media.Season}}E{{.Media.Episode}} - {{.Media.EpisodeTitle}} [{{.Quality.Resolution}}]',
    },
  },
  {
    id: 'trash',
    label: 'TRaSH Recommended',
    description: 'Based on TRaSH Guides with full quality and media info',
    movie: {
      movieDirTemplate: '{{.Media.CleanTitle}} ({{.Media.Year}}) {tmdb-{{.Media.TmdbID}}}',
      template:
        '{{.Media.CleanTitle}} ({{.Media.Year}}){{if .Release.Edition}} {edition-{{.Release.Edition}}}{{end}} [{{.Quality.Full}}]{{if .MediaInfo.AudioCodec}}[{{.MediaInfo.AudioCodec}} {{.MediaInfo.AudioChannels}}]{{end}}{{if .MediaInfo.HDR}}[{{.MediaInfo.HDR}}]{{end}}{{if .MediaInfo.VideoCodec}}[{{.MediaInfo.VideoCodec}}]{{end}}{{if .Release.ReleaseGroup}}-{{.Release.ReleaseGroup}}{{end}}',
    },
    series: {
      seriesShowTemplate: '{{.Media.Title}} ({{.Media.Year}})',
      seriesSeasonTemplate: 'Season {{.Media.Season}}',
      template:
        '{{.Media.Title}} - S{{.Media.Season}}E{{.Media.Episode}} - {{clean .Media.EpisodeTitle}} [{{.Quality.Full}}]{{if .MediaInfo.AudioCodec}}[{{.MediaInfo.AudioCodec}} {{.MediaInfo.AudioChannels}}]{{end}}{{if .MediaInfo.HDR}}[{{.MediaInfo.HDR}}]{{end}}{{if .MediaInfo.VideoCodec}}[{{.MediaInfo.VideoCodec}}]{{end}}{{if .Release.ReleaseGroup}}-{{.Release.ReleaseGroup}}{{end}}',
    },
  },
]
