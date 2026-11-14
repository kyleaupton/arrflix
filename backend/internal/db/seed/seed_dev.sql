-- seed test admin user (admin@example.com / admin, password 'password') with the admin role
insert into app_user (email, username, password_hash, is_active)
values ('admin@example.com', 'admin', 'v1:bcrypt:$2a$12$aMPgKNbhnjmvhWQmhU7EgO5YV45Hs9KYWNhWGGPkCg06RqlRM/tEK', true);

insert into user_role (user_id, role_id)
select u.id, r.id
from app_user u, role r
where u.username = 'admin' and r.name = 'admin';

-- The admin auto-approves via its role grants (requests.auto_approve:*); there
-- is no per-user approval row to seed.

-- seed a standard requesting user (user@example.com / user, password 'password')
-- with the built-in 'requester' role: browse + requests.create (HD), manage own,
-- but no requests.auto_approve — so its requests land pending for approval rather
-- than auto-spawning like the admin's.
insert into app_user (email, username, password_hash, is_active)
values ('user@example.com', 'user', 'v1:bcrypt:$2a$12$OYTuipyBYdhjKmNKrGOSAegP1/OW9YNMXDg3q1tu5pm0V3x4/qU6W', true);

insert into user_role (user_id, role_id)
select u.id, r.id
from app_user u, role r
where u.username = 'user' and r.name = 'requester';

-- mark the system initialized (normally flipped by the setup flow when the
-- first admin is created; the seed creates the admin directly, so set it here)
update app_setting set value_json = 'true'::jsonb where key = 'system.initialized';

-- seed libraries
insert into library (name, type, root_path, enabled, "default")
values ('Main Movie Library', 'movie', '/data/movies', true, true);

insert into library (name, type, root_path, enabled, "default")
values ('Main Series Library', 'series', '/data/tv', true, true);

-- name templates are seeded by the API on startup (NameTemplatesService.SeedDefaults)

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

