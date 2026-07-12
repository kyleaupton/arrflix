-- Email provider (singleton)

-- name: GetEmailProvider :one
select * from email_provider
limit 1;

-- name: CreateEmailProvider :one
insert into email_provider (
  provider, from_address, from_name, reply_to,
  host, port, security, auth, username, password,
  skip_tls_verify, config_json, enabled
)
values (
  sqlc.arg(provider), sqlc.arg(from_address), sqlc.arg(from_name), sqlc.arg(reply_to),
  sqlc.arg(host), sqlc.arg(port), sqlc.arg(security), sqlc.arg(auth), sqlc.arg(username), sqlc.arg(password),
  sqlc.arg(skip_tls_verify), sqlc.arg(config_json), sqlc.arg(enabled)
)
returning *;

-- name: UpdateEmailProvider :one
update email_provider
set provider = sqlc.arg(provider),
    from_address = sqlc.arg(from_address),
    from_name = sqlc.arg(from_name),
    reply_to = sqlc.arg(reply_to),
    host = sqlc.arg(host),
    port = sqlc.arg(port),
    security = sqlc.arg(security),
    auth = sqlc.arg(auth),
    username = sqlc.arg(username),
    password = sqlc.arg(password),
    skip_tls_verify = sqlc.arg(skip_tls_verify),
    config_json = sqlc.arg(config_json),
    enabled = sqlc.arg(enabled),
    updated_at = now()
where id = sqlc.arg(id)
returning *;
