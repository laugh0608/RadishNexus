package postgres

import (
	"context"
	"github.com/jackc/pgx/v5"
	cp "github.com/laugh0608/RadishNexus/server/internal/contextpackage"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"github.com/laugh0608/RadishNexus/server/internal/platform/entityref"
	"strconv"
	"time"
)

func (s *ContextStore) ApplyContext(ctx context.Context, a goldenpath.OwnerAccess, b cp.Bundle, expected cp.Plan, confirmed string) (out cp.Result, err error) {
	d, index, err := cp.Unpack(b)
	if err != nil {
		return out, err
	}
	if confirmed == "" || expected.PlanDigest != confirmed || !cp.ValidOperation(expected.OperationID) {
		return out, authz.ErrConflict
	}
	err = s.contextTransaction(ctx, pgx.ReadCommitted, func(tx pgx.Tx) (bool, error) {
		if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(739140967704145)`); e != nil {
			return false, ownerDBError("serialize context import", e)
		}
		session, e := contextOwner(ctx, tx, a)
		if e != nil {
			return false, e
		}
		if e = s.contextSchema(ctx, tx); e != nil {
			return false, e
		}
		identity, e := contextIdentity(ctx, tx, a.Principal.WorkspaceID)
		if e != nil {
			return false, e
		}
		plan, e := cp.BuildPlan(b.Manifest, d, a.Principal.WorkspaceID, identity, a.Principal.ID, expected.OperationID)
		if e != nil {
			return false, e
		}
		if string(cp.Encode(plan)) != string(cp.Encode(expected)) {
			return false, authz.ErrConflict
		}
		out, e = contextResult(ctx, tx, a, plan.OperationID)
		if e != nil {
			return false, e
		}
		if out.Status == "applied" {
			if out.PlanDigest != plan.PlanDigest || out.PackageDigest != plan.PackageDigest {
				return false, authz.ErrConflict
			}
			_, e = ownerSessionNow(ctx, tx, session, true)
			return false, e
		}
		if e = emptyContextTarget(ctx, tx, true); e != nil {
			return false, e
		}
		now, e := ownerSessionNow(ctx, tx, session, true)
		if e != nil {
			return false, e
		}
		now = now.UTC()
		if e = writeContext(ctx, tx, a, plan, b.Manifest, d, index, now); e != nil {
			return false, e
		}
		if _, e = ownerSessionNow(ctx, tx, session, true); e != nil {
			return false, e
		}
		out, e = contextResult(ctx, tx, a, plan.OperationID)
		return true, e
	})
	return
}

type contextIDs map[string]string

func (ids contextIDs) get(kind, id string) string { return ids[cp.MappingKey(kind, id)] }
func (ids contextIDs) optional(kind string, id *string) *string {
	if id == nil {
		return nil
	}
	mapped := ids.get(kind, *id)
	return &mapped
}
func (ids contextIDs) facts(f cp.Facts) cp.Facts {
	for _, ptr := range []**cp.Ref{&f.Evidence, &f.Decision, &f.Ticket, &f.Channel, &f.Thread, &f.Message} {
		if *ptr != nil {
			r := **ptr
			r.ID = ids.get(r.Type, r.ID)
			*ptr = &r
		}
	}
	return f
}
func writeContext(ctx context.Context, tx pgx.Tx, a goldenpath.OwnerAccess, p cp.Plan, m cp.Manifest, d cp.Data, index *cp.Index, now time.Time) error {
	ids := contextIDs{}
	for _, v := range p.Mappings {
		ids[cp.MappingKey(v.Kind, v.SourceID)] = v.TargetID
	}
	w := a.Principal.WorkspaceID
	project := ids.get("project", m.ProjectID)
	op := p.OperationID
	exec := func(q string, args ...any) error {
		_, e := tx.Exec(ctx, q, args...)
		if e != nil {
			return ownerDBError("write imported context", e)
		}
		return nil
	}
	if e := exec(`INSERT INTO radishnexus.context_import_batches(operation_id,workspace_id,actor_id,package_id,package_digest,plan_digest,target_identity,project_id,applied_at,permission_policy) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, op, w, a.Principal.ID, m.PackageID, p.PackageDigest, p.PlanDigest, p.TargetIdentity, project, now, p.Policy); e != nil {
		return e
	}
	lineages := map[string][]cp.Origin{}
	actorIDs := map[string]string{}
	visibility := map[string]string{}
	for _, o := range d.Objects {
		k := cp.MappingKey(o.Type, o.ID)
		lineages[k] = o.Lineage
		if o.Actor != "" {
			actorIDs[k] = ids.get("actor", o.Actor)
		}
		switch v := index.Decoded[cp.Ref{Type: o.Type, ID: o.ID}].(type) {
		case cp.Project:
			visibility[k] = v.Visibility
		case cp.Channel:
			visibility[k] = v.Visibility
		case cp.Thread:
			visibility[k] = v.Visibility
		case cp.Document:
			visibility[k] = v.Visibility
		}
	}
	for _, v := range d.Actors {
		lineages[cp.MappingKey("actor", v.ID)] = v.Lineage
	}
	for _, v := range d.History {
		lineages[cp.MappingKey("event", v.ID)] = v.Lineage
	}
	for _, v := range d.Links {
		k := cp.MappingKey("entity-link", v.ID)
		lineages[k] = v.Lineage
		actorIDs[k] = ids.get("actor", v.Actor)
	}
	for _, v := range p.Mappings {
		k := cp.MappingKey(v.Kind, v.SourceID)
		lineage := append(append([]cp.Origin{}, lineages[k]...), cp.Origin{PackageID: m.PackageID, Digest: p.PackageDigest, SourceID: v.SourceID})
		if len(lineage) > cp.MaxLineage {
			return cp.ErrInvalid
		}
		if e := exec(`INSERT INTO radishnexus.context_import_mappings(workspace_id,operation_id,kind,source_id,target_id,source_actor_id,source_visibility,lineage) VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),$8)`, w, op, v.Kind, v.SourceID, v.TargetID, actorIDs[k], visibility[k], cp.Encode(lineage)); e != nil {
			return e
		}
	}
	for i, v := range d.Actors {
		id := ids.get("actor", v.ID)
		if e := exec(`INSERT INTO radishnexus.users(id,display_name,created_at) VALUES($1,$2,$3)`, id, "历史导入作者 "+strconv.Itoa(i+1), now); e != nil {
			return e
		}
		if e := exec(`INSERT INTO radishnexus.user_accounts(user_id,status,created_at) VALUES($1,'disabled',$2)`, id, now); e != nil {
			return e
		}
		if e := exec(`INSERT INTO radishnexus.workspace_memberships(workspace_id,user_id,status,role,created_at) VALUES($1,$2,'suspended','member',$3)`, w, id, now); e != nil {
			return e
		}
	}
	// Stable type order satisfies ordinary foreign keys; deferred invariants still
	// validate the complete graph at COMMIT, without disabling any trigger.
	for _, kind := range []string{"team", "project", "channel", "thread", "message", "decision", "ticket", "document"} {
		for _, o := range d.Objects {
			if o.Type != kind {
				continue
			}
			id := ids.get(kind, o.ID)
			actor := ids.get("actor", o.Actor)
			v := index.Decoded[cp.Ref{Type: kind, ID: o.ID}]
			var e error
			switch data := v.(type) {
			case cp.Team:
				e = exec(`INSERT INTO radishnexus.teams(id,workspace_id,name,created_at) VALUES($1,$2,$3,$4)`, id, w, data.Name, o.CreatedAt)
			case cp.Project:
				e = exec(`INSERT INTO radishnexus.projects(id,workspace_id,key,name,summary,owner_team_id,visibility,status,created_by_kind,created_by_id,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,'restricted',$7,'import',$8,$9,$10)`, id, w, data.Key, data.Name, data.Summary, ids.get("team", data.TeamID), data.Status, op, o.CreatedAt, o.UpdatedAt)
			case cp.Channel:
				e = exec(`INSERT INTO radishnexus.channels(id,workspace_id,governing_project_id,name,visibility,status,created_by_kind,created_by_id,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,'import',$7,$8,$9)`, id, w, project, data.Name, data.Visibility, data.Status, op, o.CreatedAt, o.UpdatedAt)
			case cp.Thread:
				e = exec(`INSERT INTO radishnexus.threads(id,workspace_id,governing_project_id,title,visibility,created_by,created_at,updated_at,origin_channel_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, w, project, data.Title, data.Visibility, actor, o.CreatedAt, o.UpdatedAt, ids.optional("channel", data.ChannelID))
			case cp.Message:
				e = exec(`INSERT INTO radishnexus.messages(id,workspace_id,channel_id,thread_id,author_id,body,client_operation_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, w, ids.get("channel", data.ChannelID), ids.optional("thread", data.ThreadID), actor, data.Body, "import-"+id, o.CreatedAt)
			case cp.Decision:
				deciders := []string{}
				for _, v := range data.Deciders {
					deciders = append(deciders, ids.get("actor", v))
				}
				e = exec(`INSERT INTO radishnexus.decisions(id,workspace_id,governing_project_id,question,outcome,status,proposer_id,decider_ids,decided_at,rationale,alternatives,consequences,review_at,created_by_kind,created_by_id,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,'import',$14,$15,$16)`, id, w, project, data.Question, data.Outcome, data.Status, ids.get("actor", data.Proposer), deciders, data.DecidedAt, data.Rationale, data.Alternatives, data.Consequences, data.ReviewAt, op, o.CreatedAt, o.UpdatedAt)
			case cp.Ticket:
				e = exec(`INSERT INTO radishnexus.tickets(id,workspace_id,governing_project_id,title,status,status_revision,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, w, project, data.Title, data.Status, data.StatusRevision, actor, o.CreatedAt, o.UpdatedAt)
			case cp.Document:
				e = exec(`INSERT INTO radishnexus.documents(id,workspace_id,governing_project_id,visibility,current_revision,created_by,created_at) VALUES($1,$2,$3,'project',$4,$5,$6)`, id, w, project, data.Revision, actor, o.CreatedAt)
			}
			if e != nil {
				return e
			}
			switch kind {
			case "project":
				e = exec(`INSERT INTO radishnexus.project_memberships(workspace_id,project_id,user_id,role,created_at) VALUES($1,$2,$3,'admin',$4)`, w, id, a.Principal.ID, now)
			case "channel":
				if v.(cp.Channel).Visibility == "restricted" {
					e = exec(`INSERT INTO radishnexus.channel_memberships(workspace_id,channel_id,user_id,created_at) VALUES($1,$2,$3,$4)`, w, id, a.Principal.ID, now)
				}
			case "thread":
				if v.(cp.Thread).Visibility == "restricted" {
					e = exec(`INSERT INTO radishnexus.thread_memberships(workspace_id,thread_id,user_id,created_at) VALUES($1,$2,$3,$4)`, w, id, a.Principal.ID, now)
				}
			}
			if e != nil {
				return e
			}
		}
	}
	for _, h := range d.History {
		event := ids.get("event", h.ID)
		f := ids.facts(index.Facts[h.ID])
		if e := exec(`INSERT INTO radishnexus.context_import_history(workspace_id,operation_id,event_id,original_type,original_actor,original_time,original_facts) VALUES($1,$2,$3,$4,$5,$6,$7)`, w, op, event, h.Type, ids.get("actor", h.Actor), h.OccurredAt, cp.Encode(f)); e != nil {
			return e
		}
		if e := exec(`INSERT INTO radishnexus.domain_events(event_id,event_type,schema_version,workspace_id,actor_kind,actor_id,source_kind,source_id,primary_entity_type,primary_entity_id,project_id,correlation_id,causation_id,occurred_at,payload) VALUES($1,'context.history-recorded',1,$2,'user',$3,'import',$4,$5,$6,$7,$8,$9,$10,'{}')`, event, w, a.Principal.ID, op, h.Entity.Type, ids.get(h.Entity.Type, h.Entity.ID), project, "cor_"+cp.Hash([]byte(op + "/" + h.CorrelationID))[:32], ids.optional("event", h.CausationID), now); e != nil {
			return e
		}
	}
	for _, r := range d.Revisions {
		if e := exec(`INSERT INTO radishnexus.document_revisions(workspace_id,document_id,revision,title,body_markdown,format_version,created_by,created_at,source_event_id,restored_from_revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, w, ids.get("document", r.DocumentID), r.Revision, r.Title, d.Bodies[r.BodyFile], r.Format, ids.get("actor", r.Actor), r.CreatedAt, ids.get("event", r.EventID), r.RestoredFrom); e != nil {
			return e
		}
	}
	for _, l := range d.Links {
		var removedKind *string
		if l.RemovedActor != nil {
			v := "user"
			removedKind = &v
		}
		if e := exec(`INSERT INTO radishnexus.entity_links(id,workspace_id,from_type,from_id,relation_type,to_type,to_id,assertion,origin,origin_ref,created_by_kind,created_by_id,created_at,updated_at,source_event_id,metadata,state,removed_by_kind,removed_by_id,removed_at,removal_reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'import',$9,'import',$9,$10,$11,$12,'{}',$13,$14,$15,$16,$17)`, ids.get("entity-link", l.ID), w, l.From.Type, ids.get(l.From.Type, l.From.ID), l.Relation, l.To.Type, ids.get(l.To.Type, l.To.ID), l.Assertion, op, l.CreatedAt, l.UpdatedAt, ids.get("event", l.EventID), l.State, removedKind, ids.optional("actor", l.RemovedActor), l.RemovedAt, l.RemovalReason); e != nil {
			return e
		}
	}
	for _, h := range d.History {
		if h.Type == "message.created" || h.Type == "thread.started" {
			continue
		}
		actor := ids.get("actor", h.Actor)
		record, e := projectActivityEvent(activityEvent{eventID: ids.get("event", h.ID), eventType: h.Type, schemaVersion: 1, workspaceID: w, actorKind: "user", actorID: &actor, target: entityref.Ref{Type: h.Entity.Type, ID: ids.get(h.Entity.Type, h.Entity.ID)}, occurredAt: h.OccurredAt, payload: cp.Encode(ids.facts(index.Facts[h.ID]))})
		if e != nil {
			return e
		}
		record.safeFacts["imported"] = "true"
		if e = insertActivityRecord(ctx, tx, record); e != nil {
			return ownerDBError("project imported history", e)
		}
	}
	return nil
}
