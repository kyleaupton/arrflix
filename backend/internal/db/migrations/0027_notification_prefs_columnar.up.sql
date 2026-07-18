-- Reshape notification preferences to the subscription + outbound-channel model,
-- and give push subscriptions an honest "last notified" timestamp. See
-- specs/modules/notifications/README.md.
--
-- Pre-v1: we drop and recreate notification_preference rather than migrating rows.
-- The prior sparse (scope, value, channel) shape modeled in_app as a togglable
-- channel and carried an unused 'event' scope; the new shape makes the design
-- invariant structural instead of a convention:
--
--   * A user is *subscribed* to a bundle (the master). in_app is the inherent face
--     of a subscription — it is never a stored channel and never separately toggled;
--     "subscribed" IS "show it in the bell". Unsubscribe the bundle → silent
--     everywhere, in one boolean.
--   * email/push are outbound amplifiers layered on a subscription — meaningful only
--     while subscribed.
--
-- One row per (user, bundle). Every flag is NULLABLE: NULL = "defer to the bundle's
-- in-code registry default", so retuning a default before v1 reaches every user who
-- never overrode that flag. An absent row = pure defaults. This is the lazy model —
-- we store only genuine overrides and resolve the rest in Go (notifications.Subscribed
-- / notifications.ChannelEnabled), so there is no seeding step.
DROP TABLE IF EXISTS notification_preference;

CREATE TABLE notification_preference (
  user_id UUID NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
  bundle TEXT NOT NULL,                -- 'my_requests', 'library_activity', …
  subscribed BOOLEAN,                  -- master; in-app follows it. NULL → registry default
  email BOOLEAN,                       -- outbound amplifier. NULL → registry default
  push BOOLEAN,                        -- outbound amplifier. NULL → registry default
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, bundle)
);

-- push_subscription.last_notified_at: when this device was last successfully sent a
-- push, set by the push adapter on delivery. Distinct from last_used_at, which means
-- "last (re)subscribed/refreshed" — conflating the two made the devices UI claim
-- "last active" for a device that had only ever subscribed. NULL = never notified yet.
ALTER TABLE push_subscription ADD COLUMN last_notified_at TIMESTAMPTZ;
