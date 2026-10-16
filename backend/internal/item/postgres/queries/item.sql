-- name: CreateItem :one
INSERT INTO items (household_id, subject_id, responsible_user_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, created_by_user_id, creation_key, creation_request_fingerprint)
VALUES (sqlc.arg('household_id'), sqlc.arg('subject_id'), sqlc.narg('responsible_user_id')::uuid, sqlc.arg('title'), sqlc.narg('notes')::text, sqlc.narg('attention_on')::date, sqlc.narg('recurrence_interval_value')::integer, sqlc.narg('recurrence_interval_unit')::text, sqlc.narg('recurrence_mode')::text, sqlc.arg('created_by_user_id'), sqlc.arg('creation_key'), sqlc.arg('creation_request_fingerprint'))
ON CONFLICT (household_id, created_by_user_id, creation_key) DO NOTHING
RETURNING id::text AS id, household_id::text AS household_id, subject_id::text AS subject_id, responsible_user_id AS responsible_user_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, workflow_state, archived, done, (SELECT completed_on FROM item_completions c WHERE c.household_id = items.household_id AND c.item_id = items.id AND c.undone_at IS NULL ORDER BY c.item_version_before DESC LIMIT 1) AS last_completed_on, created_at, updated_at, version;

-- name: InsertInitializedItem :one
INSERT INTO items (household_id, subject_id, responsible_user_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, version, created_by_user_id, creation_key, creation_request_fingerprint)
VALUES (sqlc.arg('household_id'), sqlc.arg('subject_id'), sqlc.narg('responsible_user_id')::uuid, sqlc.arg('title'), sqlc.narg('notes')::text, NULL, sqlc.narg('recurrence_interval_value')::integer, sqlc.narg('recurrence_interval_unit')::text, sqlc.narg('recurrence_mode')::text, 1, sqlc.arg('created_by_user_id'), sqlc.arg('creation_key'), sqlc.arg('creation_request_fingerprint'))
ON CONFLICT (household_id, created_by_user_id, creation_key) DO NOTHING
RETURNING id::text AS id, household_id::text AS household_id, subject_id::text AS subject_id, responsible_user_id AS responsible_user_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, workflow_state, archived, done, NULL::date AS last_completed_on, created_at, updated_at, version;

-- name: FindItemCreation :one
SELECT id::text AS id, household_id::text AS household_id, subject_id::text AS subject_id, responsible_user_id AS responsible_user_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, workflow_state, archived, done, (SELECT completed_on FROM item_completions c WHERE c.household_id = items.household_id AND c.item_id = items.id AND c.undone_at IS NULL ORDER BY c.item_version_before DESC LIMIT 1) AS last_completed_on, created_at, updated_at, version, creation_request_fingerprint
FROM items
WHERE items.household_id = sqlc.arg('household_id') AND items.created_by_user_id = sqlc.arg('created_by_user_id') AND items.creation_key = sqlc.arg('creation_key');

-- name: GetItem :one
SELECT id::text AS id, household_id::text AS household_id, subject_id::text AS subject_id, responsible_user_id AS responsible_user_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, workflow_state, archived, done, (SELECT completed_on FROM item_completions c WHERE c.household_id = items.household_id AND c.item_id = items.id AND c.undone_at IS NULL ORDER BY c.item_version_before DESC LIMIT 1) AS last_completed_on, created_at, updated_at, version
FROM items
WHERE items.household_id = $1 AND items.id = $2;

-- name: ListItems :many
SELECT id::text AS id, household_id::text AS household_id, subject_id::text AS subject_id, responsible_user_id AS responsible_user_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, workflow_state, archived, done, (SELECT completed_on FROM item_completions c WHERE c.household_id = items.household_id AND c.item_id = items.id AND c.undone_at IS NULL ORDER BY c.item_version_before DESC LIMIT 1) AS last_completed_on, created_at, updated_at, version
FROM items
WHERE items.household_id = sqlc.arg('household_id')
  AND items.archived = sqlc.arg('archived')
  AND items.done = sqlc.arg('done')
  AND items.id > sqlc.arg('after_id')
  AND (sqlc.narg('subject_id')::uuid IS NULL OR items.subject_id = sqlc.narg('subject_id')::uuid)
  AND (NOT sqlc.arg('filter_responsible')::boolean OR items.responsible_user_id IS NOT DISTINCT FROM sqlc.narg('responsible_user_id')::uuid)
  AND (sqlc.narg('workflow_state')::text IS NULL OR items.workflow_state = sqlc.narg('workflow_state')::text)
  AND (sqlc.narg('attention')::text IS NULL
       OR (sqlc.narg('attention')::text = 'needs_attention' AND (items.attention_on IS NULL OR items.attention_on <= sqlc.arg('today')::date))
       OR (sqlc.narg('attention')::text = 'upcoming' AND items.attention_on > sqlc.arg('today')::date))
  AND (sqlc.narg('recurring')::boolean IS NULL OR (items.recurrence_interval_value IS NOT NULL) = sqlc.narg('recurring')::boolean)
ORDER BY items.id ASC
LIMIT sqlc.arg('row_limit');


-- name: ListItemsByAttention :many
SELECT id::text AS id, household_id::text AS household_id, subject_id::text AS subject_id, responsible_user_id AS responsible_user_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, workflow_state, archived, done, (SELECT completed_on FROM item_completions c WHERE c.household_id = items.household_id AND c.item_id = items.id AND c.undone_at IS NULL ORDER BY c.item_version_before DESC LIMIT 1) AS last_completed_on, created_at, updated_at, version
FROM items
WHERE items.household_id = sqlc.arg('household_id')
  AND items.archived = sqlc.arg('archived')
  AND items.done = sqlc.arg('done')
  AND (
    (sqlc.narg('after_attention_on')::date IS NULL
      AND (items.attention_on IS NOT NULL OR (items.attention_on IS NULL AND items.id > sqlc.arg('after_id'))))
    OR (sqlc.narg('after_attention_on')::date IS NOT NULL
      AND (items.attention_on, items.id) > (sqlc.narg('after_attention_on')::date, sqlc.arg('after_id')))
  )
  AND (sqlc.narg('subject_id')::uuid IS NULL OR items.subject_id = sqlc.narg('subject_id')::uuid)
  AND (NOT sqlc.arg('filter_responsible')::boolean OR items.responsible_user_id IS NOT DISTINCT FROM sqlc.narg('responsible_user_id')::uuid)
  AND (sqlc.narg('workflow_state')::text IS NULL OR items.workflow_state = sqlc.narg('workflow_state')::text)
  AND (sqlc.narg('attention')::text IS NULL
       OR (sqlc.narg('attention')::text = 'needs_attention' AND (items.attention_on IS NULL OR items.attention_on <= sqlc.arg('today')::date))
       OR (sqlc.narg('attention')::text = 'upcoming' AND items.attention_on > sqlc.arg('today')::date))
  AND (sqlc.narg('recurring')::boolean IS NULL OR (items.recurrence_interval_value IS NOT NULL) = sqlc.narg('recurring')::boolean)
ORDER BY items.attention_on ASC NULLS FIRST, items.id ASC
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
