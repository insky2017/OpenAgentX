package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"openagentx/internal/domain"
)

func (r *Repository) ListActiveNetworkRuns(ctx context.Context, agentID string, limit int) ([]domain.NetworkActiveRunSnapshot, error) {
	if limit <= 0 || limit > 1000 {
		return nil, domain.ErrInvalidInput("active network run limit must be between 1 and 1000")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT r.run_id,r.task_id,r.agent_id,r.status,r.backend_id,r.worker_instance_id,w.generation,r.resolved_execution_json
		FROM run_attempts r JOIN worker_instances w ON w.worker_instance_id=r.worker_instance_id
		WHERE r.status IN ('starting','running','waiting_approval','finishing') AND (?='' OR r.agent_id=?)
		ORDER BY r.started_at DESC,r.run_id DESC LIMIT ?`, agentID, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.NetworkActiveRunSnapshot, 0)
	for rows.Next() {
		var snapshot domain.NetworkActiveRunSnapshot
		var resolvedJSON string
		if err := rows.Scan(&snapshot.RunID, &snapshot.TaskID, &snapshot.AgentID, &snapshot.Status, &snapshot.BackendID, &snapshot.WorkerInstanceID, &snapshot.WorkerGeneration, &resolvedJSON); err != nil {
			return nil, err
		}
		var resolved domain.ResolvedExecutionSpec
		if err := json.Unmarshal([]byte(resolvedJSON), &resolved); err != nil {
			return nil, domain.ErrInvalidManifest
		}
		if resolved.Spec.BackendID != "" && resolved.Spec.BackendID != snapshot.BackendID {
			return nil, domain.ErrInvalidManifest
		}
		network := resolved.Spec.Network
		snapshot.NetworkMode = network.Mode
		snapshot.NetworkProfileID = network.ProfileID
		snapshot.NetworkProfileVersion = network.ProfileVersion
		snapshot.NetworkPolicyVersion = network.PolicyVersion
		snapshot.NetworkBindingRevision = network.BindingRevision
		result = append(result, snapshot)
	}
	return result, rows.Err()
}

func (r *Repository) NetworkSecretReferenced(ctx context.Context, version string) (bool, error) {
	if err := domain.ValidateOpaqueID("network secret version", version); err != nil {
		return false, err
	}
	var referenced int
	err := r.db.QueryRowContext(ctx, `SELECT CASE WHEN EXISTS (
		SELECT 1 FROM network_profiles WHERE secret_ref=?
		UNION ALL SELECT 1 FROM network_tests WHERE secret_version=?
		UNION ALL SELECT 1 FROM network_work_items WHERE secret_version=?
		UNION ALL SELECT 1 FROM network_profile_bindings WHERE secret_version=?
		UNION ALL SELECT 1 FROM run_attempts
			WHERE status IN ('starting','running','waiting_approval','finishing')
			AND json_extract(resolved_execution_json,'$.spec.network.secret_version')=?
	) THEN 1 ELSE 0 END`, version, version, version, version, version).Scan(&referenced)
	if err != nil {
		return false, err
	}
	return referenced == 1, nil
}

func (r *Repository) GetNetworkCommandReceipt(ctx context.Context, actor, operation, key, digest string) (*domain.NetworkCommandReceipt, error) {
	if actor == "" || operation == "" || key == "" || digest == "" {
		return nil, domain.ErrInvalidInput("complete network command receipt lookup is required")
	}
	var storedDigest, result, created string
	err := r.db.QueryRowContext(ctx, `SELECT request_digest,result_json,created_at FROM network_workflow_commands
		WHERE actor_principal_id=? AND operation=? AND idempotency_key=?`, actor, operation, key).
		Scan(&storedDigest, &result, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if storedDigest != digest {
		return nil, domain.ErrIdempotencyConflict
	}
	createdAt, err := parseTime(created)
	if err != nil {
		return nil, err
	}
	return &domain.NetworkCommandReceipt{
		Actor: actor, Operation: operation, IdempotencyKey: key, RequestDigest: storedDigest,
		ResultJSON: json.RawMessage(result), CreatedAt: createdAt,
	}, nil
}

func (r *Repository) ExecuteNetworkCommand(ctx context.Context, mutation domain.NetworkCommandMutation) (*domain.NetworkCommandReceipt, error) {
	if mutation.Receipt.Actor == "" || mutation.Receipt.Operation == "" || mutation.Receipt.IdempotencyKey == "" || mutation.Receipt.RequestDigest == "" || len(mutation.Receipt.ResultJSON) == 0 || !json.Valid(mutation.Receipt.ResultJSON) {
		return nil, domain.ErrInvalidInput("complete network command receipt is required")
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var digest, result, created string
	err = tx.QueryRowContext(ctx, `SELECT request_digest,result_json,created_at FROM network_workflow_commands WHERE actor_principal_id=? AND operation=? AND idempotency_key=?`, mutation.Receipt.Actor, mutation.Receipt.Operation, mutation.Receipt.IdempotencyKey).Scan(&digest, &result, &created)
	if err == nil {
		if digest != mutation.Receipt.RequestDigest {
			return nil, domain.ErrIdempotencyConflict
		}
		at, parseErr := parseTime(created)
		if parseErr != nil {
			return nil, parseErr
		}
		return &domain.NetworkCommandReceipt{Actor: mutation.Receipt.Actor, Operation: mutation.Receipt.Operation, IdempotencyKey: mutation.Receipt.IdempotencyKey, RequestDigest: digest, ResultJSON: json.RawMessage(result), CreatedAt: at}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load network command receipt: %w", err)
	}
	if mutation.Content != nil {
		if err := mutation.Content.Validate(); err != nil {
			return nil, err
		}
		direct, _ := json.Marshal(mutation.Content.DirectIPs)
		status := string(domain.NetworkProfileDraft)
		if mutation.Head != nil && mutation.Head.State == domain.NetworkStatePublished {
			status = string(domain.NetworkProfilePublished)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO network_profiles(profile_id,version,status,mode,host,port,config_file,secret_ref,direct_ips_json,manifest_digest,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,NULL,?,?,?,?,?,?)`,
			mutation.Content.ProfileID, mutation.Content.ContentVersion, status, mutation.Content.Mode, mutation.Content.Host, mutation.Content.Port, nullableString(valueOrNil(mutation.Content.SecretVersion)), string(direct), mutation.Content.ManifestDigest, mutation.Content.CreatedBy, formatTime(mutation.Content.CreatedAt), formatTime(mutation.Content.CreatedAt))
		if err != nil {
			return nil, fmt.Errorf("insert immutable network content: %w", err)
		}
	}
	if mutation.Head != nil {
		h := mutation.Head
		if mutation.ExpectedStateRevision == 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO network_profile_heads(profile_id,current_content_version,state,state_revision,ready_test_id,published_content_version,updated_at) VALUES(?,?,?,?,?,?,?)`, h.ProfileID, h.CurrentContentVersion, h.State, h.StateRevision, nullableString(valueOrNil(h.ReadyTestID)), nullableInt64(h.PublishedContentVersion), formatTime(h.UpdatedAt))
		} else {
			result, execErr := tx.ExecContext(ctx, `UPDATE network_profile_heads SET current_content_version=?,state=?,state_revision=?,ready_test_id=?,published_content_version=?,updated_at=? WHERE profile_id=? AND state_revision=?`, h.CurrentContentVersion, h.State, h.StateRevision, nullableString(valueOrNil(h.ReadyTestID)), nullableInt64(h.PublishedContentVersion), formatTime(h.UpdatedAt), h.ProfileID, mutation.ExpectedStateRevision)
			if execErr == nil {
				if count, _ := result.RowsAffected(); count != 1 {
					execErr = domain.ErrStaleVersion
				}
			}
			err = execErr
		}
		if err != nil {
			return nil, fmt.Errorf("advance network profile state: %w", err)
		}
	}
	if mutation.Test != nil {
		i, _ := json.Marshal(mutation.Test.RuntimeIdentity)
		_, err = tx.ExecContext(ctx, `INSERT INTO network_tests(test_id,profile_id,content_version,secret_version,worker_instance_id,generation,backend_id,runtime_identity_json,state,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, mutation.Test.ID, mutation.Test.ProfileID, mutation.Test.ContentVersion, nullableString(valueOrNil(mutation.Test.SecretVersion)), mutation.Test.WorkerInstanceID, mutation.Test.Generation, mutation.Test.BackendID, string(i), mutation.Test.State, mutation.Test.CreatedBy, formatTime(mutation.Test.CreatedAt))
		if err != nil {
			return nil, fmt.Errorf("create network test: %w", err)
		}
	}
	if mutation.ModePolicy != nil {
		p := mutation.ModePolicy
		if err := p.Validate(); err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO network_mode_policies(agent_id,backend_id,policy_version,mode,manifest_digest,created_by,created_at) VALUES(?,?,?,?,?,?,?)`,
			p.AgentID, p.BackendID, p.PolicyVersion, p.Mode, p.ManifestDigest, p.CreatedBy, formatTime(p.CreatedAt))
		if err != nil {
			return nil, fmt.Errorf("create immutable network mode policy: %w", err)
		}
	}
	if mutation.CheckBindingRevision {
		var revision int64
		err = tx.QueryRowContext(ctx, `SELECT version FROM network_profile_bindings WHERE agent_id=? AND backend_id=?`, mutation.BindingAgentID, mutation.BindingBackendID).Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) {
			revision = 0
			err = nil
		}
		if err != nil {
			return nil, err
		}
		if revision != mutation.ExpectedBindingRevision {
			return nil, domain.ErrStaleVersion
		}
	}
	if mutation.ModeTest != nil {
		t := mutation.ModeTest
		i, _ := json.Marshal(t.RuntimeIdentity)
		_, err = tx.ExecContext(ctx, `INSERT INTO network_mode_tests(test_id,agent_id,backend_id,policy_version,mode,manifest_digest,worker_instance_id,generation,runtime_identity_json,binding_revision,state,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			t.ID, t.AgentID, t.BackendID, t.PolicyVersion, t.Mode, t.ManifestDigest, t.WorkerInstanceID, t.Generation, string(i), t.BindingRevision, t.State, t.CreatedBy, formatTime(t.CreatedAt))
		if err != nil {
			return nil, fmt.Errorf("create network mode test: %w", err)
		}
	}
	if mutation.Work != nil {
		i, _ := json.Marshal(mutation.Work.RuntimeIdentity)
		_, err = tx.ExecContext(ctx, `INSERT INTO network_work_items(work_id,kind,profile_id,content_version,secret_version,agent_id,backend_id,worker_instance_id,generation,binding_revision,network_mode,policy_version,manifest_digest,runtime_identity_json,state,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, mutation.Work.ID, mutation.Work.Kind, nullableString(valueOrNil(mutation.Work.ProfileID)), nullableInt64(mutation.Work.ContentVersion), nullableString(valueOrNil(mutation.Work.SecretVersion)), mutation.Work.AgentID, mutation.Work.BackendID, mutation.Work.WorkerInstanceID, mutation.Work.Generation, nullableInt64(mutation.Work.BindingRevision), nullableString(valueOrNil(string(mutation.Work.Mode))), nullableInt64(mutation.Work.PolicyVersion), nullableString(valueOrNil(mutation.Work.ManifestDigest)), string(i), mutation.Work.State, formatTime(mutation.Work.CreatedAt))
		if err != nil {
			return nil, fmt.Errorf("create network work item: %w", err)
		}
	}
	if mutation.Binding != nil {
		b := mutation.Binding
		identity, _ := json.Marshal(b.RuntimeIdentity)
		if mutation.ExpectedBindingRevision == 0 {
			b.Version = 1
			_, err = tx.ExecContext(ctx, `INSERT INTO network_profile_bindings(agent_id,backend_id,mode,profile_id,profile_version,policy_version,test_id,version,desired_status,manifest_digest,secret_version,runtime_identity_json,updated_at) VALUES(?,?,?,?,?,?,?,?,'pending',?,?,?,?)`, b.AgentID, b.BackendID, b.Mode, nullableString(valueOrNil(b.ProfileID)), nullableInt64(b.ProfileVersion), nullableInt64(b.PolicyVersion), nullableString(valueOrNil(b.TestID)), b.Version, nullableString(valueOrNil(b.ManifestDigest)), nullableString(valueOrNil(func() string {
				if b.Profile != nil {
					return b.Profile.SecretRef
				}
				return ""
			}())), string(identity), formatTime(b.UpdatedAt))
		} else {
			b.Version = mutation.ExpectedBindingRevision + 1
			secret := ""
			if b.Profile != nil {
				secret = b.Profile.SecretRef
			}
			res, execErr := tx.ExecContext(ctx, `UPDATE network_profile_bindings SET mode=?,profile_id=?,profile_version=?,policy_version=?,test_id=?,version=?,desired_status='pending',diagnostic=NULL,manifest_digest=?,secret_version=?,runtime_identity_json=?,updated_at=? WHERE agent_id=? AND backend_id=? AND version=?`, b.Mode, nullableString(valueOrNil(b.ProfileID)), nullableInt64(b.ProfileVersion), nullableInt64(b.PolicyVersion), nullableString(valueOrNil(b.TestID)), b.Version, nullableString(valueOrNil(b.ManifestDigest)), nullableString(valueOrNil(secret)), string(identity), formatTime(b.UpdatedAt), b.AgentID, b.BackendID, mutation.ExpectedBindingRevision)
			if execErr == nil {
				if count, _ := res.RowsAffected(); count != 1 {
					execErr = domain.ErrStaleVersion
				}
			}
			err = execErr
		}
		if err != nil {
			return nil, fmt.Errorf("write network binding: %w", err)
		}
	}
	if mutation.Import != nil {
		i := mutation.Import
		_, err = tx.ExecContext(ctx, `INSERT INTO network_imports(worker_instance_id,generation,backend_id,source_identity,work_id,state,created_at) VALUES(?,?,?,?,?,?,?)`, i.WorkerInstanceID, i.Generation, i.BackendID, i.SourceIdentity, i.WorkID, i.State, formatTime(i.CreatedAt))
		if err != nil {
			return nil, fmt.Errorf("create network import: %w", err)
		}
	}
	if mutation.Publication != nil {
		publication := mutation.Publication
		identity, _ := json.Marshal(publication.RuntimeIdentity)
		_, err = tx.ExecContext(ctx, `INSERT INTO network_profile_publications(profile_id,content_version,test_id,runtime_identity_json,published_by,published_at) VALUES(?,?,?,?,?,?)`, publication.ProfileID, publication.ContentVersion, publication.TestID, string(identity), publication.PublishedBy, formatTime(publication.PublishedAt))
		if err != nil {
			return nil, fmt.Errorf("record network publication: %w", err)
		}
	}
	if mutation.Event != nil {
		if err := insertJournal(ctx, tx, mutation.Event); err != nil {
			return nil, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO network_workflow_commands(actor_principal_id,operation,idempotency_key,request_digest,result_json,created_at) VALUES(?,?,?,?,?,?)`, mutation.Receipt.Actor, mutation.Receipt.Operation, mutation.Receipt.IdempotencyKey, mutation.Receipt.RequestDigest, string(mutation.Receipt.ResultJSON), formatTime(mutation.Receipt.CreatedAt))
	if err != nil {
		return nil, fmt.Errorf("store network command receipt: %w", err)
	}
	if err := r.inject(FaultBeforeCommit); err != nil {
		return nil, err
	}
	if err := commit(tx); err != nil {
		return nil, err
	}
	return &mutation.Receipt, nil
}

func nullableInt64(value int64) any {
	if value == 0 {
		return nil
	}
	return value
}

func (r *Repository) GetNetworkProfileHead(ctx context.Context, profileID string) (*domain.NetworkProfileHead, error) {
	return scanNetworkProfileHead(r.db.QueryRowContext(ctx, networkHeadQuery+` WHERE h.profile_id=?`, profileID))
}

func (r *Repository) GetNetworkProfileContent(ctx context.Context, profileID string, version int64) (*domain.NetworkProfileContent, error) {
	return scanNetworkContent(r.db.QueryRowContext(ctx, `SELECT profile_id,version,mode,host,port,COALESCE(direct_ips_json,'[]'),COALESCE(secret_ref,''),COALESCE(manifest_digest,''),created_by,created_at FROM network_profiles WHERE profile_id=? AND version=?`, profileID, version))
}

func (r *Repository) ListNetworkProfileContents(ctx context.Context, limit int) ([]domain.NetworkProfileContent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := r.db.QueryContext(ctx, `SELECT profile_id,version,mode,host,port,COALESCE(direct_ips_json,'[]'),COALESCE(secret_ref,''),COALESCE(manifest_digest,''),created_by,created_at FROM network_profiles WHERE manifest_digest IS NOT NULL AND manifest_digest<>'' ORDER BY profile_id,version DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	contents := make([]domain.NetworkProfileContent, 0)
	for rows.Next() {
		content, err := scanNetworkContent(rows)
		if err != nil {
			return nil, err
		}
		contents = append(contents, *content)
	}
	return contents, rows.Err()
}

func (r *Repository) GetNetworkPublication(ctx context.Context, profileID string, version int64) (*domain.NetworkPublication, error) {
	var publication domain.NetworkPublication
	var identityJSON, publishedAt string
	err := r.db.QueryRowContext(ctx, `SELECT profile_id,content_version,test_id,runtime_identity_json,published_by,published_at FROM network_profile_publications WHERE profile_id=? AND content_version=?`, profileID, version).Scan(&publication.ProfileID, &publication.ContentVersion, &publication.TestID, &identityJSON, &publication.PublishedBy, &publishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(identityJSON), &publication.RuntimeIdentity); err != nil {
		return nil, domain.ErrInvalidManifest
	}
	publication.PublishedAt, err = parseTime(publishedAt)
	if err != nil {
		return nil, err
	}
	return &publication, nil
}

func (r *Repository) ListNetworkPublications(ctx context.Context, limit int) ([]domain.NetworkPublication, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := r.db.QueryContext(ctx, `SELECT profile_id,content_version,test_id,runtime_identity_json,published_by,published_at FROM network_profile_publications ORDER BY published_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	publications := make([]domain.NetworkPublication, 0)
	for rows.Next() {
		var publication domain.NetworkPublication
		var identityJSON, publishedAt string
		if err := rows.Scan(&publication.ProfileID, &publication.ContentVersion, &publication.TestID, &identityJSON, &publication.PublishedBy, &publishedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(identityJSON), &publication.RuntimeIdentity); err != nil {
			return nil, domain.ErrInvalidManifest
		}
		publication.PublishedAt, err = parseTime(publishedAt)
		if err != nil {
			return nil, err
		}
		publications = append(publications, publication)
	}
	return publications, rows.Err()
}

func (r *Repository) GetNetworkTest(ctx context.Context, testID string) (*domain.NetworkTest, error) {
	tests, err := r.ListNetworkTests(ctx, "", 100)
	if err != nil {
		return nil, err
	}
	for i := range tests {
		if tests[i].ID == testID {
			return &tests[i], nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *Repository) NextNetworkModePolicyVersion(ctx context.Context, agentID, backendID string) (int64, error) {
	var version int64
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(policy_version),0)+1 FROM network_mode_policies WHERE agent_id=? AND backend_id=?`, agentID, backendID).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func (r *Repository) GetNetworkModePolicy(ctx context.Context, agentID, backendID string, version int64) (*domain.NetworkModePolicy, error) {
	var p domain.NetworkModePolicy
	var created string
	err := r.db.QueryRowContext(ctx, `SELECT agent_id,backend_id,policy_version,mode,manifest_digest,created_by,created_at FROM network_mode_policies WHERE agent_id=? AND backend_id=? AND policy_version=?`, agentID, backendID, version).
		Scan(&p.AgentID, &p.BackendID, &p.PolicyVersion, &p.Mode, &p.ManifestDigest, &p.CreatedBy, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt, err = parseTime(created)
	if err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repository) GetNetworkModeTest(ctx context.Context, testID string) (*domain.NetworkModeTest, error) {
	var t domain.NetworkModeTest
	var identity, created string
	var finished sql.NullString
	var probes string
	err := r.db.QueryRowContext(ctx, `SELECT test_id,agent_id,backend_id,policy_version,mode,manifest_digest,worker_instance_id,generation,runtime_identity_json,binding_revision,state,COALESCE(diagnostic_code,''),COALESCE(duration_ms,0),probe_results_json,created_by,created_at,finished_at FROM network_mode_tests WHERE test_id=?`, testID).
		Scan(&t.ID, &t.AgentID, &t.BackendID, &t.PolicyVersion, &t.Mode, &t.ManifestDigest, &t.WorkerInstanceID, &t.Generation, &identity, &t.BindingRevision, &t.State, &t.DiagnosticCode, &t.DurationMS, &probes, &t.CreatedBy, &created, &finished)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(identity), &t.RuntimeIdentity); err != nil {
		return nil, domain.ErrInvalidManifest
	}
	if err := json.Unmarshal([]byte(probes), &t.ProbeResults); err != nil {
		return nil, domain.ErrInvalidManifest
	}
	t.CreatedAt, err = parseTime(created)
	if err != nil {
		return nil, err
	}
	if finished.Valid {
		at, parseErr := parseTime(finished.String)
		if parseErr != nil {
			return nil, parseErr
		}
		t.FinishedAt = &at
	}
	return &t, nil
}

func (r *Repository) ListNetworkModeTests(ctx context.Context, limit int) ([]domain.NetworkModeTest, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT test_id FROM network_mode_tests ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]domain.NetworkModeTest, 0, len(ids))
	for _, id := range ids {
		test, err := r.GetNetworkModeTest(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *test)
	}
	return out, nil
}

func (r *Repository) ListNetworkProfileHeads(ctx context.Context, limit int) ([]domain.NetworkProfileHead, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := r.db.QueryContext(ctx, networkHeadQuery+` ORDER BY h.profile_id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.NetworkProfileHead{}
	for rows.Next() {
		h, err := scanNetworkProfileHead(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *h)
	}
	return result, rows.Err()
}

const networkHeadQuery = `SELECT h.profile_id,h.current_content_version,h.state,h.state_revision,COALESCE(h.ready_test_id,''),COALESCE(h.published_content_version,0),h.updated_at,p.mode,p.host,p.port,COALESCE(p.direct_ips_json,'[]'),COALESCE(p.secret_ref,''),COALESCE(p.manifest_digest,''),p.created_by,p.created_at FROM network_profile_heads h JOIN network_profiles p ON p.profile_id=h.profile_id AND p.version=h.current_content_version`

func scanNetworkProfileHead(row rowScanner) (*domain.NetworkProfileHead, error) {
	var h domain.NetworkProfileHead
	var c domain.NetworkProfileContent
	var updated, created, direct string
	if err := row.Scan(&h.ProfileID, &h.CurrentContentVersion, &h.State, &h.StateRevision, &h.ReadyTestID, &h.PublishedContentVersion, &updated, &c.Mode, &c.Host, &c.Port, &direct, &c.SecretVersion, &c.ManifestDigest, &c.CreatedBy, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	h.UpdatedAt, _ = parseTime(updated)
	c.ProfileID = h.ProfileID
	c.ContentVersion = h.CurrentContentVersion
	c.CreatedAt, _ = parseTime(created)
	_ = json.Unmarshal([]byte(direct), &c.DirectIPs)
	if c.ManifestDigest == "" {
		c.LegacySecretStale = true
	} else if err := c.Validate(); err != nil {
		return nil, err
	}
	h.Content = &c
	return &h, nil
}

func (r *Repository) ListNetworkTests(ctx context.Context, profileID string, limit int) ([]domain.NetworkTest, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT test_id,profile_id,content_version,COALESCE(secret_version,''),worker_instance_id,generation,backend_id,runtime_identity_json,state,COALESCE(diagnostic_code,''),COALESCE(duration_ms,0),probe_results_json,created_by,created_at,finished_at FROM network_tests WHERE (?='' OR profile_id=?) ORDER BY created_at DESC LIMIT ?`, profileID, profileID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.NetworkTest{}
	for rows.Next() {
		var t domain.NetworkTest
		var identity, probes, created string
		var finished sql.NullString
		if err := rows.Scan(&t.ID, &t.ProfileID, &t.ContentVersion, &t.SecretVersion, &t.WorkerInstanceID, &t.Generation, &t.BackendID, &identity, &t.State, &t.DiagnosticCode, &t.DurationMS, &probes, &t.CreatedBy, &created, &finished); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(identity), &t.RuntimeIdentity)
		if err := json.Unmarshal([]byte(probes), &t.ProbeResults); err != nil {
			return nil, domain.ErrInvalidManifest
		}
		t.CreatedAt, _ = parseTime(created)
		if finished.Valid {
			x, _ := parseTime(finished.String)
			t.FinishedAt = &x
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repository) ClaimNetworkWork(ctx context.Context, guard domain.WorkerWriteGuard) (*domain.NetworkWork, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := loadGuardedWorker(ctx, tx, guard); err != nil {
		return nil, err
	}
	var w domain.NetworkWork
	var kind, identity, created string
	var profile, secret sql.NullString
	var mode, manifest sql.NullString
	var contentVersion, binding, policyVersion sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT work_id,kind,profile_id,content_version,secret_version,agent_id,backend_id,worker_instance_id,generation,binding_revision,network_mode,policy_version,manifest_digest,runtime_identity_json,state,created_at FROM network_work_items WHERE worker_instance_id=? AND generation=? AND state='pending' ORDER BY created_at LIMIT 1`, guard.WorkerInstanceID, guard.Generation).Scan(&w.ID, &kind, &profile, &contentVersion, &secret, &w.AgentID, &w.BackendID, &w.WorkerInstanceID, &w.Generation, &binding, &mode, &policyVersion, &manifest, &identity, &w.State, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	w.Kind = domain.NetworkWorkKind(kind)
	w.ProfileID = profile.String
	w.ContentVersion = contentVersion.Int64
	w.SecretVersion = secret.String
	w.BindingRevision = binding.Int64
	w.Mode = domain.NetworkMode(mode.String)
	w.PolicyVersion = policyVersion.Int64
	w.ManifestDigest = manifest.String
	if w.Mode == "" && w.ProfileID != "" {
		w.Mode = domain.NetworkNamedProfile
	}
	_ = json.Unmarshal([]byte(identity), &w.RuntimeIdentity)
	w.CreatedAt, _ = parseTime(created)
	res, err := tx.ExecContext(ctx, `UPDATE network_work_items SET state='claimed' WHERE work_id=? AND state='pending'`, w.ID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, domain.ErrStaleVersion
	}
	w.State = "claimed"
	if w.ProfileID != "" {
		head, err := scanNetworkContent(tx.QueryRowContext(ctx, `SELECT profile_id,version,mode,host,port,COALESCE(direct_ips_json,'[]'),COALESCE(secret_ref,''),COALESCE(manifest_digest,''),created_by,created_at FROM network_profiles WHERE profile_id=? AND version=?`, w.ProfileID, w.ContentVersion))
		if err != nil {
			return nil, err
		}
		w.Content = head
	}
	if err := commit(tx); err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *Repository) GetNetworkWorkForAck(ctx context.Context, guard domain.WorkerWriteGuard, workID string) (*domain.NetworkWork, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := loadGuardedWorker(ctx, tx, guard); err != nil {
		return nil, err
	}
	var w domain.NetworkWork
	var kind, identity, created string
	var profile, secret sql.NullString
	var mode, manifest sql.NullString
	var contentVersion, binding, policyVersion sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT work_id,kind,profile_id,content_version,secret_version,agent_id,backend_id,worker_instance_id,generation,binding_revision,network_mode,policy_version,manifest_digest,runtime_identity_json,state,created_at FROM network_work_items WHERE work_id=? AND worker_instance_id=? AND generation=?`, workID, guard.WorkerInstanceID, guard.Generation).Scan(&w.ID, &kind, &profile, &contentVersion, &secret, &w.AgentID, &w.BackendID, &w.WorkerInstanceID, &w.Generation, &binding, &mode, &policyVersion, &manifest, &identity, &w.State, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	w.Kind = domain.NetworkWorkKind(kind)
	w.ProfileID = profile.String
	w.ContentVersion = contentVersion.Int64
	w.SecretVersion = secret.String
	w.BindingRevision = binding.Int64
	w.Mode = domain.NetworkMode(mode.String)
	w.PolicyVersion = policyVersion.Int64
	w.ManifestDigest = manifest.String
	if w.Mode == "" && w.ProfileID != "" {
		w.Mode = domain.NetworkNamedProfile
	}
	if err := json.Unmarshal([]byte(identity), &w.RuntimeIdentity); err != nil {
		return nil, domain.ErrInvalidManifest
	}
	w.CreatedAt, err = parseTime(created)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func scanNetworkContent(row rowScanner) (*domain.NetworkProfileContent, error) {
	var c domain.NetworkProfileContent
	var direct, created string
	if err := row.Scan(&c.ProfileID, &c.ContentVersion, &c.Mode, &c.Host, &c.Port, &direct, &c.SecretVersion, &c.ManifestDigest, &c.CreatedBy, &created); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(direct), &c.DirectIPs)
	c.CreatedAt, _ = parseTime(created)
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) AcknowledgeNetworkWork(ctx context.Context, guard domain.WorkerWriteGuard, workID, state, diagnostic string, durationMS int64, probeResults []domain.NetworkProbeResult, policy *domain.NetworkPolicy, imported *domain.NetworkProfileContent, importSourceIdentity string, event *domain.JournalEvent) error {
	if state != "succeeded" && state != "failed" {
		return domain.ErrInvalidInput("invalid network work result")
	}
	if !domain.ValidNetworkDiagnostic(diagnostic) || (state == "succeeded" && diagnostic != "") {
		return domain.ErrInvalidInput("invalid network diagnostic")
	}
	encodedProbes, err := json.Marshal(probeResults)
	if err != nil {
		return domain.ErrInvalidInput("invalid network probe results")
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := loadGuardedWorker(ctx, tx, guard); err != nil {
		return err
	}
	var kind, profileID, backendID, currentState, mode, workManifest string
	var version, binding, policyVersion int64
	err = tx.QueryRowContext(ctx, `SELECT kind,COALESCE(profile_id,''),COALESCE(content_version,0),backend_id,COALESCE(binding_revision,0),COALESCE(network_mode,''),COALESCE(policy_version,0),COALESCE(manifest_digest,''),state FROM network_work_items WHERE work_id=? AND worker_instance_id=? AND generation=?`, workID, guard.WorkerInstanceID, guard.Generation).Scan(&kind, &profileID, &version, &backendID, &binding, &mode, &policyVersion, &workManifest, &currentState)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	if mode == "" && profileID != "" {
		mode = string(domain.NetworkNamedProfile)
	}
	if currentState == state {
		return nil
	}
	if currentState != "claimed" {
		return domain.ErrStaleVersion
	}
	if kind == string(domain.NetworkWorkTest) {
		if err := domain.ValidateNetworkProbeResults(state, probeResults); err != nil {
			return err
		}
	} else if len(probeResults) != 0 {
		return domain.ErrInvalidInput("probe results require network test work")
	}
	now := guard.CheckedAt
	result, err := tx.ExecContext(ctx, `UPDATE network_work_items SET state=?,diagnostic_code=?,finished_at=? WHERE work_id=? AND state='claimed'`, state, nullableString(valueOrNil(diagnostic)), formatTime(now), workID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return domain.ErrStaleVersion
	}
	if kind == string(domain.NetworkWorkTest) {
		if profileID == "" {
			result, err = tx.ExecContext(ctx, `UPDATE network_mode_tests SET state=?,diagnostic_code=?,duration_ms=?,probe_results_json=?,finished_at=? WHERE test_id=? AND worker_instance_id=? AND generation=? AND state IN ('pending','claimed')`, state, nullableString(valueOrNil(diagnostic)), durationMS, string(encodedProbes), formatTime(now), workID, guard.WorkerInstanceID, guard.Generation)
		} else {
			result, err = tx.ExecContext(ctx, `UPDATE network_tests SET state=?,diagnostic_code=?,duration_ms=?,probe_results_json=?,finished_at=? WHERE test_id=? AND worker_instance_id=? AND generation=? AND state IN ('pending','claimed')`, state, nullableString(valueOrNil(diagnostic)), durationMS, string(encodedProbes), formatTime(now), workID, guard.WorkerInstanceID, guard.Generation)
		}
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count != 1 {
			return domain.ErrStaleVersion
		}
		if profileID == "" {
			// Mode tests prove one immutable candidate only. They never mutate the
			// authoritative binding or the live Backend.
		} else if state == "succeeded" {
			result, err = tx.ExecContext(ctx, `UPDATE network_profile_heads SET state='ready',state_revision=state_revision+1,ready_test_id=?,updated_at=? WHERE profile_id=? AND current_content_version=? AND state='testing'`, workID, formatTime(now), profileID, version)
		} else {
			result, err = tx.ExecContext(ctx, `UPDATE network_profile_heads SET state='draft',state_revision=state_revision+1,ready_test_id=NULL,updated_at=? WHERE profile_id=? AND current_content_version=? AND state='testing'`, formatTime(now), profileID, version)
		}
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count != 1 {
			return domain.ErrStaleVersion
		}
	}
	if kind == string(domain.NetworkWorkApply) {
		applicationState := "failed"
		if state == "succeeded" {
			applicationState = "applied"
		}
		if applicationState == "applied" {
			if policy == nil || policy.ConfigFile != "" || policy.BlackIPFile != "" || policy.Mode != domain.NetworkMode(mode) || policy.BindingRevision != binding {
				return domain.ErrInvalidInput("network apply receipt policy does not match work")
			}
			var bindingMode, desiredProfileID, manifest, secret, identityJSON string
			var desiredProfileVersion, desiredPolicyVersion int64
			if err := tx.QueryRowContext(ctx, `SELECT mode,COALESCE(profile_id,''),COALESCE(profile_version,0),COALESCE(policy_version,0),COALESCE(manifest_digest,''),COALESCE(secret_version,''),COALESCE(runtime_identity_json,'') FROM network_profile_bindings WHERE agent_id=? AND backend_id=? AND version=?`, guard.AgentID, backendID, binding).Scan(&bindingMode, &desiredProfileID, &desiredProfileVersion, &desiredPolicyVersion, &manifest, &secret, &identityJSON); err != nil {
				return err
			}
			var identity domain.RuntimeIdentity
			_ = json.Unmarshal([]byte(identityJSON), &identity)
			if bindingMode != mode || policy.ManifestDigest != manifest || policy.SecretVersion != secret || policy.RuntimeIdentity != identity ||
				policy.ProfileID != desiredProfileID || policy.ProfileVersion != desiredProfileVersion || policy.PolicyVersion != desiredPolicyVersion ||
				policy.ManifestDigest != workManifest || policy.PolicyVersion != policyVersion {
				return domain.ErrStaleVersion
			}
			result, err = tx.ExecContext(ctx, `UPDATE network_profile_bindings SET desired_status='applied',applied_worker_id=?,applied_generation=?,applied_mode=mode,applied_profile_id=profile_id,applied_profile_version=profile_version,applied_policy_version=policy_version,applied_binding_revision=version,diagnostic=NULL,updated_at=? WHERE agent_id=? AND backend_id=? AND version=?`, guard.WorkerInstanceID, guard.Generation, formatTime(now), guard.AgentID, backendID, binding)
			if err == nil {
				if count, _ := result.RowsAffected(); count != 1 {
					err = domain.ErrStaleVersion
				}
			}
			if err == nil {
				encoded, _ := json.Marshal(policy)
				result, err = tx.ExecContext(ctx, `UPDATE runtime_backend_registrations SET network_json=?,observed_at=? WHERE worker_instance_id=? AND backend_id=?`, string(encoded), formatTime(now), guard.WorkerInstanceID, backendID)
				if err == nil {
					if count, _ := result.RowsAffected(); count != 1 {
						err = domain.ErrStaleVersion
					}
				}
			}
		} else {
			result, err = tx.ExecContext(ctx, `UPDATE network_profile_bindings SET desired_status='failed',diagnostic=?,updated_at=? WHERE agent_id=? AND backend_id=? AND version=?`, domain.NetworkApplyFailedDiagnostic, formatTime(now), guard.AgentID, backendID, binding)
			if err == nil {
				if count, _ := result.RowsAffected(); count != 1 {
					err = domain.ErrStaleVersion
				}
			}
		}
		if err != nil {
			return err
		}
	}
	if kind == string(domain.NetworkWorkImport) {
		if state == "succeeded" {
			if imported == nil || importSourceIdentity == "" || imported.ProfileID != profileID || imported.ContentVersion != 1 {
				return domain.ErrInvalidInput("network import receipt does not match work")
			}
			if err := imported.Validate(); err != nil {
				return err
			}
			direct, _ := json.Marshal(imported.DirectIPs)
			if _, err = tx.ExecContext(ctx, `INSERT INTO network_profiles(profile_id,version,status,mode,host,port,config_file,secret_ref,direct_ips_json,manifest_digest,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,NULL,?,?,?,?,?,?)`, imported.ProfileID, imported.ContentVersion, domain.NetworkProfileDraft, imported.Mode, imported.Host, imported.Port, nullableString(valueOrNil(imported.SecretVersion)), string(direct), imported.ManifestDigest, imported.CreatedBy, formatTime(imported.CreatedAt), formatTime(imported.CreatedAt)); err != nil {
				return fmt.Errorf("store imported network profile: %w", err)
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO network_profile_heads(profile_id,current_content_version,state,state_revision,updated_at) VALUES(?,1,'draft',1,?)`, imported.ProfileID, formatTime(now)); err != nil {
				return fmt.Errorf("store imported network profile head: %w", err)
			}
			result, err = tx.ExecContext(ctx, `UPDATE network_imports SET source_identity=?,profile_id=?,content_version=1,state='succeeded',finished_at=? WHERE work_id=? AND state='pending'`, importSourceIdentity, imported.ProfileID, formatTime(now), workID)
		} else {
			result, err = tx.ExecContext(ctx, `UPDATE network_imports SET state='failed',finished_at=? WHERE work_id=? AND state='pending'`, formatTime(now), workID)
		}
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count != 1 {
			return domain.ErrStaleVersion
		}
	} else if imported != nil || importSourceIdentity != "" {
		return domain.ErrInvalidInput("import receipt fields require import work")
	}
	if event != nil {
		if err := insertJournal(ctx, tx, event); err != nil {
			return err
		}
	}
	return commit(tx)
}

func (r *Repository) GetWorkerRuntimeTarget(ctx context.Context, workerID, backendID string, wantedMode domain.NetworkMode) (domain.WorkerInstance, domain.RuntimeIdentity, error) {
	worker, err := r.GetWorkerInstance(ctx, workerID)
	if err != nil {
		return domain.WorkerInstance{}, domain.RuntimeIdentity{}, err
	}
	var descriptor string
	err = r.db.QueryRowContext(ctx, `SELECT descriptor_json FROM runtime_backend_registrations WHERE worker_instance_id=? AND backend_id=?`, workerID, backendID).Scan(&descriptor)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WorkerInstance{}, domain.RuntimeIdentity{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.WorkerInstance{}, domain.RuntimeIdentity{}, err
	}
	var parsed struct {
		RuntimeIdentity domain.RuntimeIdentity `json:"runtime_identity"`
		NetworkModes    []string               `json:"network_modes"`
	}
	if err := json.Unmarshal([]byte(descriptor), &parsed); err != nil {
		return domain.WorkerInstance{}, domain.RuntimeIdentity{}, domain.ErrInvalidManifest
	}
	supported := false
	for _, mode := range parsed.NetworkModes {
		if mode == string(wantedMode) {
			supported = true
			break
		}
	}
	if !supported || parsed.RuntimeIdentity.IsZero() {
		return domain.WorkerInstance{}, domain.RuntimeIdentity{}, domain.ErrUnsupportedCapability
	}
	return *worker, parsed.RuntimeIdentity, nil
}

func (r *Repository) GetNetworkBinding(ctx context.Context, agentID, backendID string) (*domain.NetworkBinding, error) {
	return getNetworkBindingWithQueryer(ctx, r.db, agentID, backendID)
}

var _ = time.Time{}
