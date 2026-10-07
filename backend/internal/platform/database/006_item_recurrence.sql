-- +goose Up
ALTER TABLE items
 ADD COLUMN recurrence_interval_value integer NULL CHECK (recurrence_interval_value BETWEEN 1 AND 999),
 ADD COLUMN recurrence_interval_unit text NULL CHECK (recurrence_interval_unit IN ('day','week','month','year')),
 ADD COLUMN recurrence_mode text NULL CHECK (recurrence_mode IN ('fixed','after_completion')),
 ADD CONSTRAINT items_recurrence_all_or_none CHECK (
  (recurrence_interval_value IS NULL AND recurrence_interval_unit IS NULL AND recurrence_mode IS NULL)
  OR (recurrence_interval_value IS NOT NULL AND recurrence_interval_unit IS NOT NULL AND recurrence_mode IS NOT NULL)
 );
GRANT UPDATE (recurrence_interval_value, recurrence_interval_unit, recurrence_mode) ON items TO tendo;

-- +goose Down
REVOKE UPDATE (recurrence_interval_value, recurrence_interval_unit, recurrence_mode) ON items FROM tendo;
ALTER TABLE items DROP CONSTRAINT items_recurrence_all_or_none;
ALTER TABLE items DROP COLUMN recurrence_interval_value, DROP COLUMN recurrence_interval_unit, DROP COLUMN recurrence_mode;
