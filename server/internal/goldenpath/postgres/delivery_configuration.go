package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/laugh0608/RadishNexus/server/internal/goldenpath"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
)

// Lock accounts, then memberships, in stable order before any Environment lock.
// An ineligible subject can still be cleaned up or have an old receipt read.
func authorizationActors(ctx context.Context, tx pgx.Tx, p authz.Principal, user string) (owner, eligible bool, err error) {
	ids := []string{p.ID}
	if user != p.ID {
		ids = append(ids, user)
	}
	slices.Sort(ids)
	accounts := map[string]bool{}
	for _, id := range ids {
		var status string
		err = tx.QueryRow(ctx, `SELECT status FROM radishnexus.user_accounts WHERE user_id=$1 FOR SHARE`, id).Scan(&status)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, false, fmt.Errorf("lock authorization account: %w", err)
		}
		accounts[id] = err == nil && status == "active"
	}
	for _, id := range ids {
		var status, role string
		err = tx.QueryRow(ctx, `SELECT status,role FROM radishnexus.workspace_memberships WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, p.WorkspaceID, id).Scan(&status, &role)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, false, fmt.Errorf("lock authorization membership: %w", err)
		}
		active := err == nil && status == "active" && accounts[id]
		if id == p.ID {
			if !active {
				return false, false, authz.ErrNotFound
			}
			owner = role == "owner"
		}
		if id == user {
			eligible = active
		}
	}
	return owner, eligible, nil
}

func deliveryConfigurationObject(ctx context.Context, tx pgx.Tx, workspace, kind, id string, owner, write bool) (o goldenpath.ConfigurationObject, err error) {
	o.ID, o.Kind = id, kind
	lock := "FOR SHARE"
	if write {
		lock = "FOR UPDATE"
	}
	if kind == "component" {
		err = tx.QueryRow(ctx, `SELECT key,name,type,lifecycle,COALESCE(owner_team_id,'') FROM radishnexus.components WHERE workspace_id=$1 AND id=$2 `+lock, workspace, id).Scan(&o.Key, &o.Name, &o.Type, &o.Status, &o.OwnerTeamID)
	} else {
		err = tx.QueryRow(ctx, `SELECT key,name,classification,status,owner_team_id FROM radishnexus.environments WHERE workspace_id=$1 AND id=$2 `+lock, workspace, id).Scan(&o.Key, &o.Name, &o.Classification, &o.Status, &o.OwnerTeamID)
		o.CanManage = owner && o.Classification == "staging"
		o.CanGrant = o.CanManage && o.Status == "active"
		o.CanRevoke = o.CanManage
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return o, authz.ErrNotFound
	}
	if err != nil {
		return o, fmt.Errorf("load delivery configuration: %w", err)
	}
	return o, nil
}

func createDeliveryConfiguration(ctx context.Context, tx pgx.Tx, c goldenpath.ConfigurationCommand) (goldenpath.ConfigurationObject, error) {
	var team string
	err := tx.QueryRow(ctx, `SELECT id FROM radishnexus.teams WHERE workspace_id=$1 AND id=$2 FOR SHARE`, c.Principal.WorkspaceID, c.OwnerTeamID).Scan(&team)
	if errors.Is(err, pgx.ErrNoRows) {
		return goldenpath.ConfigurationObject{}, authz.ErrNotFound
	}
	if err != nil {
		return goldenpath.ConfigurationObject{}, fmt.Errorf("load delivery responsibility Team: %w", err)
	}
	kind := "component"
	if c.Kind == "component.create" {
		_, err = tx.Exec(ctx, `INSERT INTO radishnexus.components(id,workspace_id,key,name,type,owner_team_id,lifecycle,created_by_kind,created_by_id,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,'active','user',$7,$8,$8)`, c.ID, c.Principal.WorkspaceID, c.Key, c.Name, c.Delivery.Type, c.OwnerTeamID, c.Principal.ID, c.OccurredAt)
	} else {
		kind = "environment"
		_, err = tx.Exec(ctx, `INSERT INTO radishnexus.environments(id,workspace_id,key,name,classification,owner_team_id,status,created_by_kind,created_by_id,created_at,updated_at) VALUES($1,$2,$3,$4,'staging',$5,'active','user',$6,$7,$7)`, c.ID, c.Principal.WorkspaceID, c.Key, c.Name, c.OwnerTeamID, c.Principal.ID, c.OccurredAt)
	}
	if err != nil {
		return goldenpath.ConfigurationObject{}, mapDatabaseError("create delivery configuration", err)
	}
	return deliveryConfigurationObject(ctx, tx, c.Principal.WorkspaceID, kind, c.ID, true, false)
}

func latestAuthorization(ctx context.Context, tx pgx.Tx, workspace, environment, user string) (*goldenpath.ConfigurationAuthorization, int64, error) {
	a := &goldenpath.ConfigurationAuthorization{}
	var generation int64
	err := tx.QueryRow(ctx, `SELECT id,status,generation FROM radishnexus.environment_deployment_authorizations WHERE workspace_id=$1 AND environment_id=$2 AND user_id=$3 ORDER BY generation DESC LIMIT 1 FOR UPDATE`, workspace, environment, user).Scan(&a.ID, &a.Status, &generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("load latest authorization: %w", err)
	}
	return a, generation, nil
}

func configureEnvironmentAuthorization(ctx context.Context, tx pgx.Tx, c goldenpath.ConfigurationCommand, eligible bool) (before, after, authorization string, changed bool, err error) {
	a, generation, err := latestAuthorization(ctx, tx, c.Principal.WorkspaceID, c.ScopeID, c.UserID)
	if err != nil {
		return "", "", "", false, err
	}
	if a == nil && !eligible {
		return "", "", "", false, authz.ErrNotFound
	}
	expected := c.Delivery.ExpectedAuthorization
	if (a == nil) != (expected == nil) || (a != nil && *a != *expected) {
		return "", "", "", false, authz.ErrConflict
	}
	if a != nil {
		before, authorization = a.Status, a.ID
	}
	after = before
	if c.Kind == "environment.authorization.grant" {
		if !eligible {
			return "", "", "", false, authz.ErrNotFound
		}
		if before != "active" {
			after, authorization, changed = "active", c.AuthorizationID, true
			_, err = tx.Exec(ctx, `INSERT INTO radishnexus.environment_deployment_authorizations(id,workspace_id,environment_id,user_id,status,granted_by,granted_at,generation) VALUES($1,$2,$3,$4,'active',$5,$6,$7)`, authorization, c.Principal.WorkspaceID, c.ScopeID, c.UserID, c.Principal.ID, c.OccurredAt, generation+1)
		}
	} else if before == "active" {
		after, changed = "revoked", true
		_, err = tx.Exec(ctx, `UPDATE radishnexus.environment_deployment_authorizations SET status='revoked',revoked_by=$1,revoked_at=$2 WHERE workspace_id=$3 AND id=$4`, c.Principal.ID, c.OccurredAt, c.Principal.WorkspaceID, authorization)
	}
	if err != nil {
		err = mapDatabaseError("change environment authorization", err)
	}
	return
}
