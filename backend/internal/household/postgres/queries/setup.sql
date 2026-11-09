-- name: CreateHousehold :one
INSERT INTO households (name, timezone) VALUES ($1, $2) RETURNING id::text;

-- name: AddAdmin :exec
INSERT INTO household_memberships (user_id, household_id, role) VALUES ($1, $2, 'admin');
