-- Web Push: VAPID identity (singleton) and per-session subscriptions.

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

-- ClearPushForSessionOrEndpoint frees the subscribe slot before an insert: it
-- removes this session's existing push row (a re-subscribe or rotated endpoint) and
-- any row elsewhere holding this endpoint (the same browser re-homing to a new
-- login). Paired with a plain insert this enforces the 1:1 session<->push invariant
-- and re-homes an endpoint, without a fragile data-modifying-CTE upsert.
-- name: ClearPushForSessionOrEndpoint :execrows
delete from push_subscription
where session_id = sqlc.arg(session_id) or endpoint = sqlc.arg(endpoint);

-- name: InsertPushSubscription :one
insert into push_subscription (session_id, endpoint, p256dh, auth, user_agent)
values (sqlc.arg(session_id), sqlc.arg(endpoint), sqlc.arg(p256dh), sqlc.arg(auth), sqlc.arg(user_agent))
returning *;

-- ListPushSubscriptionsByUser is the fan-out lookup: every live subscription for a
-- recipient. Joining user_session means only non-revoked, non-expired sessions'
-- devices receive push — revoking a session silently stops its push, with no
-- change to the adapter.
-- name: ListPushSubscriptionsByUser :many
select ps.* from push_subscription ps
join user_session us on us.id = ps.session_id
where us.user_id = sqlc.arg(user_id)
  and us.revoked_at is null
  and us.refresh_expires_at > now()
order by ps.created_at;

-- GetPushSubscriptionByIDForUser loads one of the caller's own devices by id,
-- scoped to the owner through the session join so a test-send can never address
-- another user's subscription by guessing an id.
-- name: GetPushSubscriptionByIDForUser :one
select ps.* from push_subscription ps
join user_session us on us.id = ps.session_id
where ps.id = sqlc.arg(id) and us.user_id = sqlc.arg(user_id);

-- name: DeletePushSubscriptionByEndpoint :execrows
delete from push_subscription
where endpoint = sqlc.arg(endpoint);

-- DeletePushSubscriptionByIDForUser removes one of the caller's own devices by id,
-- scoped to the owner via the session join.
-- name: DeletePushSubscriptionByIDForUser :execrows
delete from push_subscription ps
using user_session us
where ps.session_id = us.id
  and ps.id = sqlc.arg(id)
  and us.user_id = sqlc.arg(user_id);

-- MarkPushSubscriptionNotified stamps last_notified_at when the push adapter
-- successfully sends to this endpoint. Best-effort (a lost stamp only means the
-- devices UI shows a slightly stale "last notified"); keyed by endpoint, the natural
-- key the adapter has in hand per send.
-- name: MarkPushSubscriptionNotified :exec
update push_subscription
set last_notified_at = now()
where endpoint = sqlc.arg(endpoint);
