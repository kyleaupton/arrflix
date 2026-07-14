-- Web Push: VAPID identity (singleton) and per-browser subscriptions.

-- name: GetVAPIDConfig :one
select * from vapid_config
limit 1;

-- name: CreateVAPIDConfig :one
insert into vapid_config (public_key, private_key, subject)
values (sqlc.arg(public_key), sqlc.arg(private_key), sqlc.arg(subject))
returning *;

-- name: UpdateVAPIDSubject :one
update vapid_config
set subject = sqlc.arg(subject),
    updated_at = now()
where id = sqlc.arg(id)
returning *;

-- UpsertPushSubscription registers (or refreshes) a browser's subscription.
-- endpoint is the natural key: a re-subscribe from the same browser re-uses the
-- endpoint, so we refresh its keys/owner/user_agent and bump last_used_at rather
-- than inserting a duplicate. This also re-homes an endpoint if a different user
-- signs in on the same browser.
-- name: UpsertPushSubscription :one
insert into push_subscription (user_id, endpoint, p256dh, auth, user_agent)
values (sqlc.arg(user_id), sqlc.arg(endpoint), sqlc.arg(p256dh), sqlc.arg(auth), sqlc.arg(user_agent))
on conflict (endpoint) do update
set user_id = excluded.user_id,
    p256dh = excluded.p256dh,
    auth = excluded.auth,
    user_agent = excluded.user_agent,
    last_used_at = now()
returning *;

-- name: ListPushSubscriptionsByUser :many
select * from push_subscription
where user_id = sqlc.arg(user_id)
order by created_at;

-- name: DeletePushSubscriptionByEndpoint :execrows
delete from push_subscription
where endpoint = sqlc.arg(endpoint);

-- DeletePushSubscriptionForUser scopes deletion to the owner so one user cannot
-- unsubscribe another's device by guessing an endpoint.
-- name: DeletePushSubscriptionForUser :execrows
delete from push_subscription
where endpoint = sqlc.arg(endpoint) and user_id = sqlc.arg(user_id);

-- name: TouchPushSubscription :exec
update push_subscription
set last_used_at = now()
where endpoint = sqlc.arg(endpoint);
