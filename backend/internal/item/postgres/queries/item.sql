-- name: CreateItem :one
INSERT INTO items (household_id, subject_id, responsible_user_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode)
VALUES (sqlc.arg('household_id'), sqlc.arg('subject_id'), sqlc.narg('responsible_user_id')::uuid, sqlc.arg('title'), sqlc.narg('notes')::text, sqlc.narg('attention_on')::date, sqlc.narg('recurrence_interval_value')::integer, sqlc.narg('recurrence_interval_unit')::text, sqlc.narg('recurrence_mode')::text)
RETURNING id::text AS id, household_id::text AS household_id, subject_id::text AS subject_id, responsible_user_id AS responsible_user_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, workflow_state, archived, done, (SELECT completed_on FROM item_completions c WHERE c.household_id = items.household_id AND c.item_id = items.id AND c.undone_at IS NULL ORDER BY c.item_version_before DESC LIMIT 1) AS last_completed_on, created_at, updated_at, version;

-- name: InsertInitializedItem :one
INSERT INTO items (household_id, subject_id, responsible_user_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, version)
VALUES (sqlc.arg('household_id'), sqlc.arg('subject_id'), sqlc.narg('responsible_user_id')::uuid, sqlc.arg('title'), sqlc.narg('notes')::text, NULL, sqlc.narg('recurrence_interval_value')::integer, sqlc.narg('recurrence_interval_unit')::text, sqlc.narg('recurrence_mode')::text, 1)
RETURNING id::text AS id, household_id::text AS household_id, subject_id::text AS subject_id, responsible_user_id AS responsible_user_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, workflow_state, archived, done, NULL::date AS last_completed_on, created_at, updated_at, version;

-- name: GetItem :one
SELECT id::text AS id, household_id::text AS household_id, subject_id::text AS subject_id, responsible_user_id AS responsible_user_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, workflow_state, archived, done, (SELECT completed_on FROM item_completions c WHERE c.household_id = items.household_id AND c.item_id = items.id AND c.undone_at IS NULL ORDER BY c.item_version_before DESC LIMIT 1) AS last_completed_on, created_at, updated_at, version
FROM items
WHERE items.household_id = $1 AND items.id = $2;

-- name: ListItems :many
SELECT id::text AS id, household_id::text AS household_id, subject_id::text AS subject_id, responsible_user_id AS responsible_user_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, workflow_state, archived, done, (SELECT completed_on FROM item_completions c WHERE c.household_id = items.household_id AND c.item_id = items.id AND c.undone_at IS NULL ORDER BY c.item_version_before DESC LIMIT 1) AS last_completed_on, created_at, updated_at, version
FROM items
WHERE items.household_id = sqlc.arg('household_id') AND archived = sqlc.arg('archived') AND done = sqlc.arg('done') AND items.id > sqlc.arg('after_id')
ORDER BY id ASC
LIMIT sqlc.arg('row_limit');

-- name: UpdateItem :one
UPDATE items
SET title = COALESCE(sqlc.narg('title')::text, title),
    subject_id = COALESCE(sqlc.narg('subject_id')::uuid, subject_id),
    responsible_user_id = CASE WHEN sqlc.arg('set_responsible_user_id')::boolean THEN sqlc.narg('responsible_user_id')::uuid ELSE responsible_user_id END,
    notes = CASE WHEN sqlc.arg('set_notes')::boolean THEN sqlc.narg('notes')::text ELSE notes END,
    attention_on = CASE WHEN sqlc.arg('set_attention_on')::boolean THEN sqlc.narg('attention_on')::date ELSE attention_on END,
    recurrence_interval_value = CASE WHEN sqlc.arg('set_recurrence')::boolean THEN sqlc.narg('recurrence_interval_value')::integer ELSE recurrence_interval_value END,
    recurrence_interval_unit = CASE WHEN sqlc.arg('set_recurrence')::boolean THEN sqlc.narg('recurrence_interval_unit')::text ELSE recurrence_interval_unit END,
    recurrence_mode = CASE WHEN sqlc.arg('set_recurrence')::boolean THEN sqlc.narg('recurrence_mode')::text ELSE recurrence_mode END,
    workflow_state = COALESCE(sqlc.narg('workflow_state')::text, workflow_state),
    archived = COALESCE(sqlc.narg('archived')::boolean, archived),
    version = version + 1,
    updated_at = now()
WHERE items.household_id = sqlc.arg('household_id') AND items.id = sqlc.arg('id') AND version = sqlc.arg('expected_version')
RETURNING id::text AS id, household_id::text AS household_id, subject_id::text AS subject_id, responsible_user_id AS responsible_user_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, workflow_state, archived, done, (SELECT completed_on FROM item_completions c WHERE c.household_id = items.household_id AND c.item_id = items.id AND c.undone_at IS NULL ORDER BY c.item_version_before DESC LIMIT 1) AS last_completed_on, created_at, updated_at, version;
