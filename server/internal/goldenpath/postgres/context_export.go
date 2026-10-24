package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"sort"
)

// Explicit JSON constructors keep database-only fields (credentials, ACLs,
// client operation IDs, provider metadata) out of the portable profile.
var contextObjectQueries = map[string]string{
	"team":     `SELECT jsonb_build_object('type','team','id',t.id,'actor','','created_at',t.created_at,'updated_at',t.created_at,'data',jsonb_build_object('name',t.name),'lineage',COALESCE(m.lineage,'[]')) FROM radishnexus.teams t LEFT JOIN radishnexus.context_import_mappings m ON m.workspace_id=t.workspace_id AND m.kind='team' AND m.target_id=t.id WHERE t.workspace_id=$1 AND t.id=$2`,
	"project":  `SELECT jsonb_build_object('type','project','id',t.id,'actor',COALESCE(m.source_actor_id,t.created_by_id),'created_at',t.created_at,'updated_at',t.updated_at,'data',jsonb_build_object('key',t.key,'name',t.name,'summary',COALESCE(t.summary,''),'team_id',t.owner_team_id,'visibility',t.visibility,'status',t.status),'lineage',COALESCE(m.lineage,'[]')) FROM radishnexus.projects t LEFT JOIN radishnexus.context_import_mappings m ON m.workspace_id=t.workspace_id AND m.kind='project' AND m.target_id=t.id WHERE t.workspace_id=$1 AND t.id=$2`,
	"channel":  `SELECT jsonb_build_object('type','channel','id',t.id,'actor',COALESCE(m.source_actor_id,t.created_by_id),'created_at',t.created_at,'updated_at',t.updated_at,'data',jsonb_build_object('name',t.name,'visibility',t.visibility,'status',t.status),'lineage',COALESCE(m.lineage,'[]')) FROM radishnexus.channels t LEFT JOIN radishnexus.context_import_mappings m ON m.workspace_id=t.workspace_id AND m.kind='channel' AND m.target_id=t.id WHERE t.workspace_id=$1 AND t.id=$2`,
	"message":  `SELECT jsonb_build_object('type','message','id',t.id,'actor',t.author_id,'created_at',t.created_at,'updated_at',t.created_at,'data',jsonb_build_object('channel_id',t.channel_id,'thread_id',t.thread_id,'body',t.body),'lineage',COALESCE(m.lineage,'[]')) FROM radishnexus.messages t LEFT JOIN radishnexus.context_import_mappings m ON m.workspace_id=t.workspace_id AND m.kind='message' AND m.target_id=t.id WHERE t.workspace_id=$1 AND t.id=$2`,
	"thread":   `SELECT jsonb_build_object('type','thread','id',t.id,'actor',t.created_by,'created_at',t.created_at,'updated_at',t.updated_at,'data',jsonb_build_object('title',t.title,'visibility',t.visibility,'channel_id',t.origin_channel_id),'lineage',COALESCE(m.lineage,'[]')) FROM radishnexus.threads t LEFT JOIN radishnexus.context_import_mappings m ON m.workspace_id=t.workspace_id AND m.kind='thread' AND m.target_id=t.id WHERE t.workspace_id=$1 AND t.id=$2`,
	"decision": `SELECT jsonb_build_object('type','decision','id',t.id,'actor',COALESCE(m.source_actor_id,t.created_by_id),'created_at',t.created_at,'updated_at',t.updated_at,'data',jsonb_build_object('question',t.question,'outcome',t.outcome,'rationale',t.rationale,'status',t.status,'proposer',t.proposer_id,'deciders',t.decider_ids,'decided_at',t.decided_at,'alternatives',t.alternatives,'consequences',t.consequences,'review_at',t.review_at),'lineage',COALESCE(m.lineage,'[]')) FROM radishnexus.decisions t LEFT JOIN radishnexus.context_import_mappings m ON m.workspace_id=t.workspace_id AND m.kind='decision' AND m.target_id=t.id WHERE t.workspace_id=$1 AND t.id=$2`,
	"ticket":   `SELECT jsonb_build_object('type','ticket','id',t.id,'actor',t.created_by,'created_at',t.created_at,'updated_at',t.updated_at,'data',jsonb_build_object('title',t.title,'status',t.status,'status_revision',t.status_revision),'lineage',COALESCE(m.lineage,'[]')) FROM radishnexus.tickets t LEFT JOIN radishnexus.context_import_mappings m ON m.workspace_id=t.workspace_id AND m.kind='ticket' AND m.target_id=t.id WHERE t.workspace_id=$1 AND t.id=$2`,
	"document": `SELECT jsonb_build_object('type','document','id',t.id,'actor',t.created_by,'created_at',t.created_at,'updated_at',r.created_at,'data',jsonb_build_object('revision',t.current_revision,'visibility',t.visibility),'lineage',COALESCE(m.lineage,'[]')) FROM radishnexus.documents t JOIN radishnexus.document_revisions r ON r.workspace_id=t.workspace_id AND r.document_id=t.id AND r.revision=t.current_revision LEFT JOIN radishnexus.context_import_mappings m ON m.workspace_id=t.workspace_id AND m.kind='document' AND m.target_id=t.id WHERE t.workspace_id=$1 AND t.id=$2`,
}

func (s *ContextStore) ExportContext(ctx context.Context, a goldenpath.OwnerAccess, project string) (out cp.Bundle, err error) {
	err = s.contextTransaction(ctx, a.Principal, pgx.RepeatableRead, func(tx pgx.Tx) (bool, error) {
		session, _, e := lockOwnerReader(ctx, tx, a)
		if e != nil {
			return false, e
		}
		if _, e = ownerSessionNow(ctx, tx, session, true); e != nil {
			return false, e
		}
		if e = s.contextSchema(ctx, tx); e != nil {
			return false, e
		}
		access, readable, e := readProjectAccess(ctx, tx, a.Principal, project)
		if e != nil {
			return false, e
		}
		if !readable || access.role != authz.RoleAdmin {
			return false, cp.ErrIncomplete
		}
		// Team directory is part of the existing active Workspace member read contract.
		var team string
		if e = tx.QueryRow(ctx, `SELECT owner_team_id FROM radishnexus.projects WHERE workspace_id=$1 AND id=$2`, a.Principal.WorkspaceID, project).Scan(&team); e != nil {
			return false, cp.ErrIncomplete
		}
		refs, e := contextJSONRows[cp.Ref](ctx, tx, `SELECT jsonb_build_object('type',kind,'id',id) FROM (
    SELECT 'channel' kind,id FROM radishnexus.channels WHERE workspace_id=$1 AND governing_project_id=$2
    UNION ALL SELECT 'thread',id FROM radishnexus.threads WHERE workspace_id=$1 AND governing_project_id=$2
    UNION ALL SELECT 'decision',id FROM radishnexus.decisions WHERE workspace_id=$1 AND governing_project_id=$2
    UNION ALL SELECT 'ticket',id FROM radishnexus.tickets WHERE workspace_id=$1 AND governing_project_id=$2
    UNION ALL SELECT 'document',id FROM radishnexus.documents WHERE workspace_id=$1 AND governing_project_id=$2
    UNION ALL SELECT 'message',m.id FROM radishnexus.messages m JOIN radishnexus.channels c ON c.workspace_id=m.workspace_id AND c.id=m.channel_id WHERE c.workspace_id=$1 AND c.governing_project_id=$2
   ) r ORDER BY kind,id`, cp.MaxObjects, a.Principal.WorkspaceID, project)
		if e != nil {
			return false, e
		}
		chosen := []cp.Ref{{Type: "team", ID: team}, {Type: "project", ID: project}}
		for _, r := range refs {
			_, canRead, e := entityAccess(ctx, tx, a.Principal, entityref.Ref{Type: r.Type, ID: r.ID})
			if e != nil {
				return false, e
			}
			if canRead {
				chosen = append(chosen, r)
			}
		}
		d := cp.Data{Bodies: map[string]string{}, Objects: []cp.Object{}, Actors: []cp.Actor{}, Links: []cp.Link{}, History: []cp.History{}, Revisions: []cp.Revision{}}
		ids := []string{}
		actors := map[string]bool{}
		size := 0
		for _, ref := range chosen {
			items, e := contextJSONRows[cp.Object](ctx, tx, contextObjectQueries[ref.Type], 1, a.Principal.WorkspaceID, ref.ID)
			if e != nil {
				return false, e
			}
			if len(items) != 1 {
				return false, cp.ErrIncomplete
			}
			o := items[0]
			size += len(o.Data)
			if size > cp.MaxBytes {
				return false, cp.ErrIncomplete
			}
			d.Objects = append(d.Objects, o)
			ids = append(ids, ref.ID)
			if o.Actor != "" {
				actors[o.Actor] = true
			}
			if o.Type == "decision" {
				var dec cp.Decision
				if e = json.Unmarshal(o.Data, &dec); e != nil {
					return false, cp.ErrIncomplete
				}
				actors[dec.Proposer] = true
				for _, id := range dec.Deciders {
					actors[id] = true
				}
			}
		}
		d.History, e = contextJSONRows[cp.History](ctx, tx, `SELECT jsonb_build_object('id',e.event_id,'type',COALESCE(h.original_type,e.event_type),'version',e.schema_version,'entity',jsonb_build_object('type',e.primary_entity_type,'id',e.primary_entity_id),'actor',COALESCE(h.original_actor,e.actor_id),'source',e.source_kind,'occurred_at',COALESCE(h.original_time,e.occurred_at),'correlation_id',e.correlation_id,'causation_id',e.causation_id,'facts',COALESCE(h.original_facts,e.payload),'lineage',COALESCE(m.lineage,'[]')) FROM radishnexus.domain_events e LEFT JOIN radishnexus.context_import_history h ON h.workspace_id=e.workspace_id AND h.event_id=e.event_id LEFT JOIN radishnexus.context_import_mappings m ON m.workspace_id=e.workspace_id AND m.kind='event' AND m.target_id=e.event_id WHERE e.workspace_id=$1 AND e.primary_entity_id=ANY($2::text[]) AND e.event_type IN ('project.created','channel.created','message.created','thread.started','decision.proposed','decision.accepted','ticket.created','ticket.status-changed','document.created','document.revised','context.history-recorded') ORDER BY COALESCE(h.original_time,e.occurred_at),e.event_id`, cp.MaxHistory, a.Principal.WorkspaceID, ids)
		if e != nil {
			return false, e
		}
		for i, h := range d.History {
			actors[h.Actor] = true
			d.History[i].CorrelationID = "cor_" + cp.Hash([]byte(h.CorrelationID))[:32]
		}
		d.Links, e = contextJSONRows[cp.Link](ctx, tx, `SELECT jsonb_build_object('id',l.id,'from',jsonb_build_object('type',l.from_type,'id',l.from_id),'to',jsonb_build_object('type',l.to_type,'id',l.to_id),'relation',l.relation_type,'assertion',l.assertion,'origin',l.origin,'actor',COALESCE(m.source_actor_id,l.created_by_id),'created_at',l.created_at,'updated_at',l.updated_at,'event_id',l.source_event_id,'state',l.state,'removed_actor',l.removed_by_id,'removed_at',l.removed_at,'removal_reason',l.removal_reason,'lineage',COALESCE(m.lineage,'[]')) FROM radishnexus.entity_links l LEFT JOIN radishnexus.context_import_mappings m ON m.workspace_id=l.workspace_id AND m.kind='entity-link' AND m.target_id=l.id WHERE l.workspace_id=$1 AND l.from_id=ANY($2::text[]) AND l.to_id=ANY($2::text[]) AND (l.from_type,l.relation_type,l.to_type) IN (('thread','started-from','message'),('decision','derived-from','thread'),('ticket','implements','decision'),('ticket','relates-to','document')) ORDER BY l.id`, cp.MaxHistory, a.Principal.WorkspaceID, ids)
		if e != nil {
			return false, e
		}
		for _, l := range d.Links {
			actors[l.Actor] = true
			if l.RemovedActor != nil {
				actors[*l.RemovedActor] = true
			}
		}
		type revisionBody struct {
			Revision cp.Revision `json:"revision"`
			Body     string      `json:"body"`
		}
		revs, e := contextJSONRows[revisionBody](ctx, tx, `SELECT jsonb_build_object('revision',jsonb_build_object('document_id',document_id,'revision',revision,'title',title,'format',format_version,'actor',created_by,'created_at',created_at,'event_id',source_event_id,'restored_from_revision',restored_from_revision,'body_file','documents/'||document_id||'/'||revision::text||'.md'),'body',body_markdown) FROM radishnexus.document_revisions WHERE workspace_id=$1 AND document_id=ANY($2::text[]) ORDER BY document_id,revision`, cp.MaxHistory, a.Principal.WorkspaceID, ids)
		if e != nil {
			return false, e
		}
		for _, r := range revs {
			size += len(r.Body)
			if size > cp.MaxBytes {
				return false, cp.ErrIncomplete
			}
			d.Revisions = append(d.Revisions, r.Revision)
			d.Bodies[r.Revision.BodyFile] = r.Body
		}
		actorIDs := []string{}
		for id := range actors {
			actorIDs = append(actorIDs, id)
		}
		sort.Strings(actorIDs)
		for _, id := range actorIDs {
			items, e := contextJSONRows[cp.Actor](ctx, tx, `SELECT jsonb_build_object('id',u.id,'lineage',COALESCE(m.lineage,'[]')) FROM radishnexus.users u LEFT JOIN radishnexus.context_import_mappings m ON m.workspace_id=$1 AND m.kind='actor' AND m.target_id=u.id WHERE u.id=$2`, 1, a.Principal.WorkspaceID, id)
			if e != nil {
				return false, e
			}
			if len(items) != 1 {
				return false, cp.ErrIncomplete
			}
			d.Actors = append(d.Actors, items[0])
		}
		packageID, e := cp.NewID("pkg_")
		if e != nil {
			return false, e
		}
		now, e := ownerSessionNow(ctx, tx, session, true)
		if e != nil {
			return false, e
		}
		out, e = cp.Pack(cp.Manifest{PackageID: packageID, CreatedAt: now.UTC(), SourceVersion: "schema-020", WorkspaceID: a.Principal.WorkspaceID, ProjectID: project}, d)
		if e != nil {
			if errors.Is(e, cp.ErrInvalid) {
				return false, cp.ErrIncomplete
			}
			return false, e
		}
		// All authorization rows remain FOR SHARE locked through audit commit. A
		// preceding revocation causes RR serialization failure; a later one waits.
		if _, e = ownerSessionNow(ctx, tx, session, true); e != nil {
			return false, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO radishnexus.context_export_audit(package_id,workspace_id,actor_id,project_id,package_digest,occurred_at) VALUES($1,$2,$3,$4,$5,$6)`, packageID, a.Principal.WorkspaceID, a.Principal.ID, project, cp.Digest(out.Manifest), now)
		if e != nil {
			return false, ownerDBError("record context export", e)
		}
		return false, nil
	})
	if err != nil {
		out = cp.Bundle{}
	}
	return
}
