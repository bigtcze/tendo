-- name: CreateSubject :one
INSERT INTO subjects (household_id, type, name, created_by_user_id, creation_key, creation_request_fingerprint)
VALUES (sqlc.arg('household_id'), sqlc.arg('type'), sqlc.arg('name'), sqlc.arg('created_by_user_id'), sqlc.arg('creation_key'), sqlc.arg('creation_request_fingerprint'))
ON CONFLICT (household_id, created_by_user_id, creation_key) DO NOTHING
RETURNING id::text AS id, household_id::text AS household_id, type, name, archived, created_at, updated_at, version;

-- name: FindSubjectCreation :one
SELECT id::text AS id, household_id::text AS household_id, type, name, archived, created_at, updated_at, version, creation_request_fingerprint
FROM subjects
WHERE household_id = $1 AND created_by_user_id = $2 AND creation_key = $3;

-- name: GetSubject :one
SELECT id::text AS id, household_id::text AS household_id, type, name, archived, created_at, updated_at, version
FROM subjects
WHERE household_id = $1 AND id = $2;

-- name: ListSubjects :many
SELECT id::text AS id, household_id::text AS household_id, type, name, archived, created_at, updated_at, version
FROM subjects
WHERE household_id = sqlc.arg('household_id') AND archived = sqlc.arg('archived') AND id > sqlc.arg('after_id')
ORDER BY id ASC
LIMIT sqlc.arg('row_limit');

-- name: UpdateSubject :one
UPDATE subjects
SET name = COALESCE(sqlc.narg('name')::text, name),
    type = COALESCE(sqlc.narg('type')::text, type),
    archived = COALESCE(sqlc.narg('archived')::boolean, archived),
    version = version + 1,
    updated_at = now()
WHERE household_id = sqlc.arg('household_id') AND id = sqlc.arg('id') AND version = sqlc.arg('expected_version')
RETURNING id::text AS id, household_id::text AS household_id, type, name, archived, created_at, updated_at, version;
