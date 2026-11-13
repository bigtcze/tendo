-- +goose Up
-- V5-03: persist the recurrence anchor day next to the (possibly clamped)
-- attention date so fixed month/year schedules restore the original day instead
-- of drifting (Jan 31 -> Feb 28 -> Mar 31).
--
-- The anchor is the day of month 1..31 the current cycle is aligned to. On
-- items it always accompanies attention_on; NULL and NULL go together. Receipts
-- snapshot the cycle anchor before the completion and the next anchor the engine
-- produced. NULL anchors on a receipt identify a pre-V5 (legacy) receipt.

ALTER TABLE items ADD COLUMN attention_anchor_day smallint NULL;
ALTER TABLE item_completions ADD COLUMN cycle_anchor_day smallint NULL;
ALTER TABLE item_completions ADD COLUMN next_anchor_day smallint NULL;
GRANT UPDATE (attention_anchor_day) ON items TO tendo;

-- Conservative backfill: the default is the stored attention day, which is exact
-- whenever no clamp ever happened. attention_on and receipts are never rewritten.
UPDATE items SET attention_anchor_day = extract(day from attention_on)::smallint WHERE attention_on IS NOT NULL;

-- Exact recovery only for items where the stored day can only be a clamp and the
-- receipt chain proves the original anchor: fixed month/year, day 28..30 (or
-- Feb 28/29 for yearly), a month-end date, no undone receipt, versions 1..n with
-- no gap, items.version = last+1, each receipt linked to the next, and the last
-- next date equal to the stored attention date. The recovered anchor is the first
-- receipt's cycle day (or its completed day when that cycle date is NULL) and is
-- used only when it is later than the stored day.
UPDATE items i
SET attention_anchor_day = recovery.anchor_day
FROM (
    SELECT c.household_id, c.item_id,
           COALESCE(extract(day FROM first.cycle_attention_on), extract(day FROM first.completed_on))::smallint AS anchor_day,
           count(*) AS total,
           min(c.item_version_before) AS first_version,
           max(c.item_version_before) AS last_version,
           count(*) FILTER (WHERE c.undone_at IS NOT NULL) AS undone
    FROM item_completions c
    JOIN LATERAL (
        SELECT cc.cycle_attention_on, cc.completed_on
        FROM item_completions cc
        WHERE cc.household_id = c.household_id AND cc.item_id = c.item_id
        ORDER BY cc.item_version_before
        LIMIT 1
    ) first ON true
    GROUP BY c.household_id, c.item_id, first.cycle_attention_on, first.completed_on
) recovery
WHERE recovery.household_id = i.household_id
  AND recovery.item_id = i.id
  AND i.attention_on IS NOT NULL
  AND i.recurrence_interval_unit IN ('month','year')
  AND i.recurrence_mode = 'fixed'
  AND (
      (i.recurrence_interval_unit = 'month' AND extract(day FROM i.attention_on) IN (28,29,30))
      OR (i.recurrence_interval_unit = 'year' AND extract(month FROM i.attention_on) = 2 AND extract(day FROM i.attention_on) IN (28,29))
  )
  AND i.attention_on = (date_trunc('month', i.attention_on) + interval '1 month - 1 day')::date
  AND recovery.undone = 0
  AND recovery.first_version = 1
  AND recovery.last_version - recovery.first_version + 1 = recovery.total
  AND i.version = recovery.last_version + 1
  AND NOT EXISTS (
      SELECT 1
      FROM item_completions c
      JOIN item_completions nxt
        ON nxt.household_id = c.household_id
       AND nxt.item_id = c.item_id
       AND nxt.item_version_before = c.item_version_before + 1
      WHERE c.household_id = i.household_id
        AND c.item_id = i.id
        AND c.next_attention_on IS DISTINCT FROM nxt.cycle_attention_on
  )
  AND NOT EXISTS (
      SELECT 1
      FROM item_completions c
      WHERE c.household_id = i.household_id
        AND c.item_id = i.id
        AND c.item_version_before = recovery.last_version
        AND c.next_attention_on IS DISTINCT FROM i.attention_on
  )
  AND recovery.anchor_day > extract(day FROM i.attention_on);

ALTER TABLE items ADD CONSTRAINT items_attention_anchor_all_or_none
  CHECK ((attention_on IS NULL) = (attention_anchor_day IS NULL));
ALTER TABLE items ADD CONSTRAINT items_attention_anchor_range
  CHECK (attention_anchor_day IS NULL OR (attention_anchor_day BETWEEN 1 AND 31 AND attention_anchor_day >= extract(day FROM attention_on)));
ALTER TABLE items ADD CONSTRAINT items_attention_anchor_clamp
  CHECK (attention_anchor_day IS NULL
      OR attention_anchor_day = extract(day FROM attention_on)
      OR attention_on = (date_trunc('month', attention_on) + interval '1 month - 1 day')::date);
ALTER TABLE item_completions ADD CONSTRAINT item_completions_cycle_anchor_day_range
  CHECK (cycle_anchor_day IS NULL OR cycle_anchor_day BETWEEN 1 AND 31);
ALTER TABLE item_completions ADD CONSTRAINT item_completions_next_anchor_day_range
  CHECK (next_anchor_day IS NULL OR next_anchor_day BETWEEN 1 AND 31);

-- +goose Down
-- Lossy by design: dropping the anchor columns returns fixed month/year schedules
-- to the pre-V5 sequential-clamp drift (Jan 31 -> Feb 28 -> Mar 28). attention_on
-- and receipts are unchanged.
ALTER TABLE item_completions DROP CONSTRAINT item_completions_next_anchor_day_range;
ALTER TABLE item_completions DROP CONSTRAINT item_completions_cycle_anchor_day_range;
ALTER TABLE items DROP CONSTRAINT items_attention_anchor_clamp;
ALTER TABLE items DROP CONSTRAINT items_attention_anchor_range;
ALTER TABLE items DROP CONSTRAINT items_attention_anchor_all_or_none;
REVOKE UPDATE (attention_anchor_day) ON items FROM tendo;
ALTER TABLE item_completions DROP COLUMN next_anchor_day;
ALTER TABLE item_completions DROP COLUMN cycle_anchor_day;
ALTER TABLE items DROP COLUMN attention_anchor_day;
