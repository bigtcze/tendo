-- name: ReleaseItemsResponsibleByMember :exec
-- Removing a member releases their responsibility on every item of the
-- household, archived and done items included. The item rows keep their
-- subject, history and schedule; only the responsible member is cleared.
UPDATE items SET responsible_user_id=NULL, version=version+1, updated_at=now()
WHERE household_id=$1 AND responsible_user_id=$2;
