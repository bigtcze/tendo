-- name: UnlinkMemberPersonSubject :exec
-- Removing a member unlinks the canonical person subject without archiving it:
-- the person stays active and keeps item and history associations, and only
-- the login link disappears. A later rejoin provisions a fresh person; nothing
-- is merged by name (ADR 0010, ADR 0019).
UPDATE subjects SET linked_user_id=NULL, version=version+1, updated_at=now()
WHERE household_id=$1 AND linked_user_id=$2;
