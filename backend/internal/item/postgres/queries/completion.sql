-- name: LockItemForCompletion :one
SELECT id::text AS id, household_id::text AS household_id, subject_id::text AS subject_id, title, notes, attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, workflow_state, archived, done, (SELECT completed_on FROM item_completions c WHERE c.household_id = items.household_id AND c.item_id = items.id ORDER BY c.item_version_before DESC LIMIT 1) AS last_completed_on, created_at, updated_at, version FROM items WHERE items.household_id=$1 AND items.id=$2 FOR UPDATE;

-- name: GetCompletionByKey :one
SELECT id::text AS id, household_id::text AS household_id, item_id::text AS item_id, completed_on, completed_by_user_id::text AS completed_by_user_id, cycle_attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, prior_workflow_state, next_attention_on, item_version_before, idempotency_key, request_fingerprint, created_at FROM item_completions WHERE household_id=$1 AND item_id=$2 AND idempotency_key=$3;

-- name: InsertCompletion :one
INSERT INTO item_completions (household_id,item_id,completed_on,completed_by_user_id,cycle_attention_on,recurrence_interval_value,recurrence_interval_unit,recurrence_mode,prior_workflow_state,next_attention_on,item_version_before,idempotency_key,request_fingerprint) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id::text AS id, household_id::text AS household_id, item_id::text AS item_id, completed_on, completed_by_user_id::text AS completed_by_user_id, cycle_attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, prior_workflow_state, next_attention_on, item_version_before, idempotency_key, request_fingerprint, created_at;

-- name: UpdateItemForCompletion :exec
UPDATE items SET attention_on=$3, workflow_state=$4, done=$5, version=version+1, updated_at=now() WHERE household_id=$1 AND id=$2;

-- name: ListCompletions :many
SELECT id::text AS id, household_id::text AS household_id, item_id::text AS item_id, completed_on, completed_by_user_id::text AS completed_by_user_id, cycle_attention_on, recurrence_interval_value, recurrence_interval_unit, recurrence_mode, prior_workflow_state, next_attention_on, item_version_before, idempotency_key, request_fingerprint, created_at FROM item_completions WHERE household_id=$1 AND item_id=$2 AND id>$3 ORDER BY id ASC LIMIT $4;
