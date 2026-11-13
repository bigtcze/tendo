-- name: FindMemberHousehold :one
SELECT h.id::text AS id, h.name, h.timezone, h.created_at, h.version, m.role AS viewer_role
FROM households h
JOIN household_memberships m ON m.household_id = h.id
WHERE h.id = $1 AND m.user_id = $2;

-- name: ListMemberHouseholds :many
SELECT h.id::text AS id, h.name, h.timezone, h.created_at, m.role AS viewer_role
FROM households h
JOIN household_memberships m ON m.household_id = h.id
WHERE m.user_id=$1 AND ($2::uuid IS NULL OR h.id > $2)
ORDER BY h.id LIMIT $3;
