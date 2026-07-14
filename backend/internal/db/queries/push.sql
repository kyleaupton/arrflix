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

-- GetPushSubscriptionByIDForUser loads one of the caller's own devices by id.
-- Scoping the read to the owner means a test-send or delete can never address
-- another user's subscription by guessing an id.
-- name: GetPushSubscriptionByIDForUser :one
select * from push_subscription
where id = sqlc.arg(id) and user_id = sqlc.arg(user_id);

-- name: DeletePushSubscriptionByEndpoint :execrows
delete from push_subscription
where endpoint = sqlc.arg(endpoint);

-- DeletePushSubscriptionByIDForUser removes one of the caller's own devices by
-- id, scoped to the owner so a user cannot delete another's subscription.
-- name: DeletePushSubscriptionByIDForUser :execrows
delete from push_subscription
where id = sqlc.arg(id) and user_id = sqlc.arg(user_id);

-- MarkPushSubscriptionNotified stamps last_notified_at when the push adapter
-- successfully sends to this endpoint. Best-effort (a lost stamp only means the
-- devices UI shows a slightly stale "last notified"); keyed by endpoint, the natural
-- key the adapter has in hand per send.
-- name: MarkPushSubscriptionNotified :exec
update push_subscription
set last_notified_at = now()
where endpoint = sqlc.arg(endpoint);
