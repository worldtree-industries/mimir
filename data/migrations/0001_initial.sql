-- Configs, the Items they declare, the Namespaces they're used in, and the
-- Overrides set on their Items. Times come from mimir-server's clock, not
-- now(), so expiry can be tested.

CREATE TABLE configs (
    name          text PRIMARY KEY,
    display_name  text NOT NULL DEFAULT '',
    registered_at timestamptz NOT NULL
);

-- An Item is identified by its Config, name and type together (ADR 0004).
CREATE TABLE items (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    config              text NOT NULL REFERENCES configs (name) ON DELETE CASCADE,
    name                text NOT NULL,
    type                text NOT NULL,
    description         text NOT NULL,
    default_value       text NOT NULL,
    first_registered_at timestamptz NOT NULL,
    last_registered_at  timestamptz NOT NULL,
    UNIQUE (config, name, type)
);

CREATE TABLE namespaces (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    config              text NOT NULL REFERENCES configs (name) ON DELETE CASCADE,
    deployable          text NOT NULL,
    environment         text NOT NULL,
    first_registered_at timestamptz NOT NULL,
    last_registered_at  timestamptz NOT NULL,
    UNIQUE (config, deployable, environment)
);

-- A NULL deployable or environment means the Scope isn't narrowed by it.
CREATE TABLE overrides (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    item_id     bigint NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    deployable  text,
    environment text,
    value       text NOT NULL,
    created_at  timestamptz NOT NULL,
    updated_at  timestamptz NOT NULL,
    UNIQUE NULLS NOT DISTINCT (item_id, deployable, environment)
);
