-- seed test admin user (admin@example.com / admin, password 'password') with the admin role
insert into app_user (email, username, password_hash, is_active)
values ('admin@example.com', 'admin', 'v1:bcrypt:$2a$12$aMPgKNbhnjmvhWQmhU7EgO5YV45Hs9KYWNhWGGPkCg06RqlRM/tEK', true);

insert into user_role (user_id, role_id)
select u.id, r.id
from app_user u, role r
where u.username = 'admin' and r.name = 'admin';

-- The admin auto-approves via its role grants (requests.auto_approve:*); there
-- is no per-user approval row to seed.

-- mark the system initialized (normally flipped by the setup flow when the
-- first admin is created; the seed creates the admin directly, so set it here)
update app_setting set value_json = 'true'::jsonb where key = 'system.initialized';

-- seed libraries
insert into library (name, type, root_path, enabled, "default")
values ('Main Movie Library', 'movie', '/data/movies', true, true);

insert into library (name, type, root_path, enabled, "default")
values ('Main Series Library', 'series', '/data/tv', true, true);

-- seed name templates
insert into name_template (name, type, template, movie_dir_template, "default")
values ('Main Movie Template', 'movie', '{{.Media.CleanTitle}} ({{.Media.Year}}){{if .Identity.Edition}} {edition-{{.Identity.Edition}}}{{end}} [{{.Quality.Full}}]{{if .MediaInfo.AudioCodec}}[{{.MediaInfo.AudioCodec}} {{.MediaInfo.AudioChannels}}]{{end}}{{if .MediaInfo.HDR}}[{{.MediaInfo.HDR}}]{{end}}{{if .MediaInfo.VideoCodec}}[{{.MediaInfo.VideoCodec}}]{{end}}{{if .Encode.ReleaseGroup}}-{{.Encode.ReleaseGroup}}{{end}}', '{{.Media.CleanTitle}} ({{.Media.Year}}) {tmdb-{{.Media.TmdbID}}}', true);

insert into name_template (name, type, template, series_show_template, series_season_template, "default")
values ('Main Series Template', 'series', '{{.Media.Title}} - S{{.Media.Season}}E{{.Media.Episode}} - {{clean .Media.EpisodeTitle}} [{{.Quality.Full}}]{{if .MediaInfo.AudioCodec}}[{{.MediaInfo.AudioCodec}} {{.MediaInfo.AudioChannels}}]{{end}}{{if .MediaInfo.HDR}}[{{.MediaInfo.HDR}}]{{end}}{{if .MediaInfo.VideoCodec}}[{{.MediaInfo.VideoCodec}}]{{end}}{{if .Encode.ReleaseGroup}}-{{.Encode.ReleaseGroup}}{{end}}', '{{.Media.Title}} ({{.Media.Year}})', 'Season {{.Media.Season}}', true);

-- seed downloaders
-- downloader_password is injected by `just db-reseed` from .env (kept out of
-- git); falls back to 'admin' when this file is run directly.
\if :{?downloader_password}
\else
  \set downloader_password 'admin'
\endif

insert into downloader (name, type, protocol, url, username, password, enabled, "default")
values ('Main Downloader', 'qbittorrent', 'torrent', 'http://172.16.10.22:8480', 'admin', :'downloader_password', true, true);

-- seed tmdb api key
-- tmdb_api_key is injected by `just db-reseed` from TMDB_API_KEY in .env (kept
-- out of git). Omitted entirely when unset so the app falls back to its own
-- TMDB_API_KEY env var.
\if :{?tmdb_api_key}
insert into app_setting (key, type, value_json)
values ('tmdb.api_key', 'text', to_jsonb(:'tmdb_api_key'::text));
\endif

