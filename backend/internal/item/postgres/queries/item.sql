-- name: CreateItem :one
INSERT INTO items (household_id, subject_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode)
VALUES (sqlc.arg('household_id'), sqlc.arg('subject_id'), sqlc.arg('title'), sqlc.narg('notes')::text, sqlc.narg('attention_on')::date, sqlc.narg('recurrence_interval_value')::integer, sqlc.narg('recurrence_interval_unit')::text, sqlc.narg('recurrence_mode')::text)
RETURNING id::text AS id, household_id::text AS household_id, subject_id::text AS subject_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, workflow_state, archived, created_at, updated_at, version;

-- name: GetItem :one
SELECT id::text AS id, household_id::text AS household_id, subject_id::text AS subject_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, workflow_state, archived, created_at, updated_at, version
FROM items
WHERE household_id = $1 AND id = $2;

-- name: ListItems :many
SELECT id::text AS id, household_id::text AS household_id, subject_id::text AS subject_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, workflow_state, archived, created_at, updated_at, version
FROM items
WHERE household_id = sqlc.arg('household_id') AND archived = sqlc.arg('archived') AND id > sqlc.arg('after_id')
ORDER BY id ASC
LIMIT sqlc.arg('row_limit');

-- name: UpdateItem :one
UPDATE items
SET title = COALESCE(sqlc.narg('title')::text, title),
    subject_id = COALESCE(sqlc.narg('subject_id')::uuid, subject_id),
    notes = CASE WHEN sqlc.arg('set_notes')::boolean THEN sqlc.narg('notes')::text ELSE notes END,
    attention_on = CASE WHEN sqlc.arg('set_attention_on')::boolean THEN sqlc.narg('attention_on')::date ELSE attention_on END,
    recurrence_interval_value = CASE WHEN sqlc.arg('set_recurrence')::boolean THEN sqlc.narg('recurrence_interval_value')::integer ELSE recurrence_interval_value END,
    recurrence_interval_unit = CASE WHEN sqlc.arg('set_recurrence')::boolean THEN sqlc.narg('recurrence_interval_unit')::text ELSE recurrence_interval_unit END,
    recurrence_mode = CASE WHEN sqlc.arg('set_recurrence')::boolean THEN sqlc.narg('recurrence_mode')::text ELSE recurrence_mode END,
    workflow_state = COALESCE(sqlc.narg('workflow_state')::text, workflow_state),
    archived = COALESCE(sqlc.narg('archived')::boolean, archived),
    version = version + 1,
    updated_at = now()
WHERE household_id = sqlc.arg('household_id') AND id = sqlc.arg('id') AND version = sqlc.arg('expected_version')
RETURNING id::text AS id, household_id::text AS household_id, subject_id::text AS subject_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, workflow_state, archived, created_at, updated_at, version;
