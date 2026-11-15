CREATE INDEX entity_links_nexus_incoming_page
    ON radishnexus.entity_links (workspace_id, to_type, to_id, created_at, id)
    WHERE state = 'active' AND (
        (from_type = 'decision' AND relation_type = 'derived-from' AND to_type = 'thread')
        OR (from_type = 'ticket' AND relation_type = 'implements' AND to_type = 'decision')
    );
