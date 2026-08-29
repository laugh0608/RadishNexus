CREATE TABLE radishnexus.activity_items (
    workspace_id text NOT NULL REFERENCES radishnexus.workspaces (id),
    target_type text NOT NULL REFERENCES radishnexus.entity_types (type_name),
    target_id text NOT NULL,
    event_id text NOT NULL,
    activity_type text NOT NULL,
    actor_kind text NOT NULL,
    actor_id text,
    occurred_at timestamptz NOT NULL,
    subject_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
    projection_version integer NOT NULL,
    safe_facts jsonb NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (projection_version, event_id, target_type, target_id),
    FOREIGN KEY (workspace_id, event_id)
        REFERENCES radishnexus.domain_events (workspace_id, event_id),
    CONSTRAINT activity_items_target_id CHECK (
        radishnexus.valid_entity_id(target_type, target_id)
    ),
    CONSTRAINT activity_items_type_format CHECK (
        activity_type ~ '^[a-z][a-z0-9-]*\.[a-z][a-z0-9-]*$'
    ),
    CONSTRAINT activity_items_actor CHECK (
        actor_kind IN ('user', 'system', 'plugin', 'import')
        AND (actor_kind = 'system' OR actor_id IS NOT NULL)
    ),
    CONSTRAINT activity_items_subject_refs CHECK (
        jsonb_typeof(subject_refs) = 'array'
    ),
    CONSTRAINT activity_items_projection_version CHECK (projection_version > 0),
    CONSTRAINT activity_items_safe_facts CHECK (
        jsonb_typeof(safe_facts) = 'object'
    )
);

CREATE INDEX activity_items_target_timeline
ON radishnexus.activity_items (
    workspace_id,
    target_type,
    target_id,
    projection_version,
    occurred_at,
    event_id
);

---- create above / drop below ----

DROP TABLE radishnexus.activity_items;
