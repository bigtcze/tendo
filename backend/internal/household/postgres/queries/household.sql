-- name: FindMemberHousehold :one
SELECT h.id::text AS id, h.name, h.timezone, h.created_at, h.version
FROM households h
JOIN household_memberships m ON m.household_id = h.id
WHERE h.id = $1 AND m.user_id = $2;
