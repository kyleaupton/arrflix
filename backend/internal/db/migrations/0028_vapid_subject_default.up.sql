-- Repoint the VAPID sub claim off the original mailto:admin@arrflix.local default.
--
-- Apple's push service validates the sub claim of the VAPID JWT and rejects a
-- non-routable domain with 403 BadJwtToken, so every push to an iOS device died
-- while Chrome — which does not validate sub — kept working. The default is now
-- the project URL: an https sub is always valid and needs no operator input.
--
-- The keypair itself is untouched. Only the subject was wrong, and it is read per
-- send, so existing subscriptions keep working; regenerating keys would instead
-- invalidate every one of them.
--
-- Scoped to the exact superseded default so an operator's own edited subject —
-- the settings UI writes this column — is left alone.
UPDATE vapid_config
SET subject    = 'https://github.com/kyleaupton/arrflix',
    updated_at = now()
WHERE subject = 'mailto:admin@arrflix.local';
