-- An Override removed because its Item expired is recorded as 'expired',
-- not as a 'deleted' with a special caller. Entries already recorded stay as
-- they are: history is append-only.

ALTER TABLE history DROP CONSTRAINT history_action_check;
ALTER TABLE history ADD CONSTRAINT history_action_check
    CHECK (action IN ('created', 'updated', 'deleted', 'expired'));
