-- The append-only history of Override changes. Entries copy the Scope and
-- Item rather than reference the overrides and items rows, which come and go
-- (deleted Overrides, expired Items). Time comes from mimir-server's clock.

CREATE TABLE history (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    at          timestamptz NOT NULL,
    action      text NOT NULL CHECK (action IN ('created', 'updated', 'deleted')),
    config      text NOT NULL,
    deployable  text NOT NULL DEFAULT '',
    environment text NOT NULL DEFAULT '',
    item_name   text NOT NULL,
    item_type   text NOT NULL,
    old_value   text,
    new_value   text,
    caller      text NOT NULL
);

CREATE INDEX history_config_idx ON history (config, id);

-- History is never modified or deleted, not even by a bug.
CREATE FUNCTION history_is_append_only() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'history is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER history_append_only
    BEFORE UPDATE OR DELETE ON history
    FOR EACH ROW EXECUTE FUNCTION history_is_append_only();

CREATE TRIGGER history_no_truncate
    BEFORE TRUNCATE ON history
    FOR EACH STATEMENT EXECUTE FUNCTION history_is_append_only();
