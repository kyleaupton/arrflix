-- seed dev user
insert into app_user (email, username, password_hash, is_active)
values ('dev@local.seed', 'devuser', 'v1:bcrypt:$2a$12$n180ANBjuXfZrr.hWFZXjukiDZuQ1Kw6yauaIrEHriMjempCALOB2', true);

-- seed libraries
insert into library (name, type, root_path, enabled, "default")
values ('Main Movie Library', 'movie', '/data/movies', true, true);

insert into library (name, type, root_path, enabled, "default")
values ('Main Series Library', 'series', '/data/tv', true, true);

-- seed name templates
insert into name_template (name, type, template, movie_dir_template, "default")
values ('Main Movie Template', 'movie', '{{.Media.CleanTitle}} ({{.Media.Year}}){{if .Release.Edition}} {edition-{{.Release.Edition}}}{{end}} [{{.Quality.Full}}]{{if .MediaInfo.AudioCodec}}[{{.MediaInfo.AudioCodec}} {{.MediaInfo.AudioChannels}}]{{end}}{{if .MediaInfo.HDR}}[{{.MediaInfo.HDR}}]{{end}}{{if .MediaInfo.VideoCodec}}[{{.MediaInfo.VideoCodec}}]{{end}}{{if .Release.ReleaseGroup}}-{{.Release.ReleaseGroup}}{{end}}', '{{.Media.CleanTitle}} ({{.Media.Year}}) {tmdb-{{.Media.TmdbID}}}', true);

insert into name_template (name, type, template, series_show_template, series_season_template, "default")
values ('Main Series Template', 'series', '{{.Media.Title}} - S{{.Media.Season}}E{{.Media.Episode}} - {{clean .Media.EpisodeTitle}} [{{.Quality.Full}}]{{if .MediaInfo.AudioCodec}}[{{.MediaInfo.AudioCodec}} {{.MediaInfo.AudioChannels}}]{{end}}{{if .MediaInfo.HDR}}[{{.MediaInfo.HDR}}]{{end}}{{if .MediaInfo.VideoCodec}}[{{.MediaInfo.VideoCodec}}]{{end}}{{if .Release.ReleaseGroup}}-{{.Release.ReleaseGroup}}{{end}}', '{{.Media.Title}} ({{.Media.Year}})', 'Season {{.Media.Season}}', true);

-- seed downloaders
insert into downloader (name, type, protocol, url, username, password, enabled, "default")
values ('Main Downloader', 'qbittorrent', 'torrent', 'http://172.16.10.22:8485', 'admin', 'admin', true, true);

