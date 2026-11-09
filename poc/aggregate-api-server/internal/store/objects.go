/* Copyright Contributors to the Open Cluster Management project */

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
)

func (s *Postgres) migrateObjects(ctx context.Context) error {
	const migration = `
CREATE TABLE IF NOT EXISTS poc_object_revision (
  singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
  value BIGINT NOT NULL DEFAULT 0
);
INSERT INTO poc_object_revision(singleton, value) VALUES (TRUE, 0)
  ON CONFLICT (singleton) DO NOTHING;
CREATE TABLE IF NOT EXISTS poc_objects (
  api_group TEXT NOT NULL,
  api_version TEXT NOT NULL,
  resource TEXT NOT NULL,
  namespace TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  resource_version BIGINT NOT NULL,
  body JSONB NOT NULL,
  PRIMARY KEY (api_group, api_version, resource, namespace, name)
);
CREATE TABLE IF NOT EXISTS poc_object_events (
  resource_version BIGINT PRIMARY KEY,
  api_group TEXT NOT NULL,
  api_version TEXT NOT NULL,
  resource TEXT NOT NULL,
  namespace TEXT NOT NULL DEFAULT '',
  name TEXT NOT NULL,
  event_type TEXT NOT NULL,
  body JSONB NOT NULL,
  previous_body JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS poc_object_events_watch
  ON poc_object_events (api_group, api_version, resource, namespace, resource_version);
`
	for _, statement := range splitStatements(migration) {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize generic OCM object storage: %w", err)
		}
	}
	return nil
}

func splitStatements(script string) []string {
	statements := []string{}
	for _, statement := range strings.Split(script, ";") {
		if statement = strings.TrimSpace(statement); statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}

func objectGroupResource(key ResourceKey) schema.GroupResource {
	return schema.GroupResource{Group: key.Group, Resource: key.Resource}
}

func (s *Postgres) GetObject(ctx context.Context, key ResourceKey, namespace, name string) (*unstructured.Unstructured, error) {
	var body []byte
	err := s.db.QueryRowContext(ctx, `SELECT body FROM poc_objects
WHERE api_group=$1 AND api_version=$2 AND resource=$3 AND namespace=$4 AND name=$5`,
		key.Group, key.Version, key.Resource, namespace, name).Scan(&body)
	if err == sql.ErrNoRows {
		return nil, apierrors.NewNotFound(objectGroupResource(key), name)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s %s/%s: %w", key.Resource, namespace, name, err)
	}
	return decodeObject(body)
}

func (s *Postgres) ListObjects(ctx context.Context, key ResourceKey, namespace string) ([]unstructured.Unstructured, string, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, "", fmt.Errorf("begin %s list snapshot: %w", key.Resource, err)
	}
	defer tx.Rollback()
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT value FROM poc_object_revision WHERE singleton=TRUE`).Scan(&revision); err != nil {
		return nil, "", fmt.Errorf("read OCM object list resourceVersion: %w", err)
	}
	query := `SELECT body FROM poc_objects WHERE api_group=$1 AND api_version=$2 AND resource=$3 ORDER BY namespace,name`
	args := []any{key.Group, key.Version, key.Resource}
	if namespace != "" {
		query = `SELECT body FROM poc_objects WHERE api_group=$1 AND api_version=$2 AND resource=$3 AND namespace=$4 ORDER BY namespace,name`
		args = append(args, namespace)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list %s: %w", key.Resource, err)
	}
	defer rows.Close()
	items := []unstructured.Unstructured{}
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, "", fmt.Errorf("read %s row: %w", key.Resource, err)
		}
		obj, err := decodeObject(body)
		if err != nil {
			return nil, "", err
		}
		items = append(items, *obj)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate %s rows: %w", key.Resource, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, "", fmt.Errorf("finish %s list snapshot: %w", key.Resource, err)
	}
	return items, strconv.FormatInt(revision, 10), nil
}

func (s *Postgres) CreateObject(ctx context.Context, key ResourceKey, input *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	obj := input.DeepCopy()
	obj.SetUID(types.UID(uuid.NewUUID()))
	obj.SetCreationTimestamp(metav1.NewTime(time.Now().UTC()))
	obj.SetDeletionTimestamp(nil)
	obj.SetDeletionGracePeriodSeconds(nil)
	obj.SetResourceVersion("")
	obj.SetGeneration(1)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin %s create: %w", key.Resource, err)
	}
	defer tx.Rollback()
	rv, err := nextObjectResourceVersion(ctx, tx)
	if err != nil {
		return nil, err
	}
	obj.SetResourceVersion(rv)
	body, err := json.Marshal(obj.Object)
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", key.Resource, err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO poc_objects
(api_group,api_version,resource,namespace,name,resource_version,body) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb)`,
		key.Group, key.Version, key.Resource, obj.GetNamespace(), obj.GetName(), rv, string(body))
	if err != nil {
		if isObjectUniqueViolation(err) {
			return nil, apierrors.NewAlreadyExists(objectGroupResource(key), obj.GetName())
		}
		return nil, fmt.Errorf("insert %s: %w", key.Resource, err)
	}
	if err := appendObjectEvent(ctx, tx, key, rv, obj, nil, "ADDED"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit %s create: %w", key.Resource, err)
	}
	return obj, nil
}

func (s *Postgres) UpdateObject(ctx context.Context, key ResourceKey, input *unstructured.Unstructured, statusOnly bool) (*unstructured.Unstructured, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin %s update: %w", key.Resource, err)
	}
	defer tx.Rollback()
	var body []byte
	err = tx.QueryRowContext(ctx, `SELECT body FROM poc_objects
WHERE api_group=$1 AND api_version=$2 AND resource=$3 AND namespace=$4 AND name=$5 FOR UPDATE`,
		key.Group, key.Version, key.Resource, input.GetNamespace(), input.GetName()).Scan(&body)
	if err == sql.ErrNoRows {
		return nil, apierrors.NewNotFound(objectGroupResource(key), input.GetName())
	}
	if err != nil {
		return nil, fmt.Errorf("read %s for update: %w", key.Resource, err)
	}
	existing, err := decodeObject(body)
	if err != nil {
		return nil, err
	}
	if input.GetResourceVersion() == "" || input.GetResourceVersion() != existing.GetResourceVersion() {
		return nil, apierrors.NewConflict(objectGroupResource(key), input.GetName(), fmt.Errorf("resourceVersion %q is stale; current value is %q", input.GetResourceVersion(), existing.GetResourceVersion()))
	}
	updated := input.DeepCopy()
	updated.SetUID(existing.GetUID())
	updated.SetCreationTimestamp(existing.GetCreationTimestamp())
	updated.SetDeletionTimestamp(existing.GetDeletionTimestamp())
	updated.SetDeletionGracePeriodSeconds(existing.GetDeletionGracePeriodSeconds())
	if statusOnly {
		status, found, _ := unstructured.NestedFieldCopy(input.Object, "status")
		updated.Object = existing.DeepCopy().Object
		if found {
			_ = unstructured.SetNestedField(updated.Object, status, "status")
		} else {
			unstructured.RemoveNestedField(updated.Object, "status")
		}
	} else {
		status, found, _ := unstructured.NestedFieldCopy(existing.Object, "status")
		if found {
			_ = unstructured.SetNestedField(updated.Object, status, "status")
		} else {
			unstructured.RemoveNestedField(updated.Object, "status")
		}
		oldSpec, _, _ := unstructured.NestedFieldCopy(existing.Object, "spec")
		newSpec, _, _ := unstructured.NestedFieldCopy(updated.Object, "spec")
		if !equalJSON(oldSpec, newSpec) {
			updated.SetGeneration(existing.GetGeneration() + 1)
		} else {
			updated.SetGeneration(existing.GetGeneration())
		}
	}
	rv, err := nextObjectResourceVersion(ctx, tx)
	if err != nil {
		return nil, err
	}
	updated.SetResourceVersion(rv)
	updatedBody, err := json.Marshal(updated.Object)
	if err != nil {
		return nil, fmt.Errorf("encode updated %s: %w", key.Resource, err)
	}
	eventType := "MODIFIED"
	if updated.GetDeletionTimestamp() != nil && len(updated.GetFinalizers()) == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM poc_objects WHERE api_group=$1 AND api_version=$2 AND resource=$3 AND namespace=$4 AND name=$5`, key.Group, key.Version, key.Resource, updated.GetNamespace(), updated.GetName()); err != nil {
			return nil, fmt.Errorf("complete finalized %s deletion: %w", key.Resource, err)
		}
		eventType = "DELETED"
	} else if _, err := tx.ExecContext(ctx, `UPDATE poc_objects SET resource_version=$1,body=$2::jsonb
WHERE api_group=$3 AND api_version=$4 AND resource=$5 AND namespace=$6 AND name=$7`, rv, string(updatedBody), key.Group, key.Version, key.Resource, updated.GetNamespace(), updated.GetName()); err != nil {
		return nil, fmt.Errorf("update %s row: %w", key.Resource, err)
	}
	if err := appendObjectEvent(ctx, tx, key, rv, updated, existing, eventType); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit %s update: %w", key.Resource, err)
	}
	return updated, nil
}

func (s *Postgres) DeleteObject(ctx context.Context, key ResourceKey, namespace, name, expectedRV string) (*unstructured.Unstructured, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin %s delete: %w", key.Resource, err)
	}
	defer tx.Rollback()
	var body []byte
	err = tx.QueryRowContext(ctx, `SELECT body FROM poc_objects
WHERE api_group=$1 AND api_version=$2 AND resource=$3 AND namespace=$4 AND name=$5 FOR UPDATE`,
		key.Group, key.Version, key.Resource, namespace, name).Scan(&body)
	if err == sql.ErrNoRows {
		return nil, apierrors.NewNotFound(objectGroupResource(key), name)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s for delete: %w", key.Resource, err)
	}
	obj, err := decodeObject(body)
	if err != nil {
		return nil, err
	}
	if expectedRV != "" && expectedRV != obj.GetResourceVersion() {
		return nil, apierrors.NewConflict(objectGroupResource(key), name, fmt.Errorf("resourceVersion %q is stale; current value is %q", expectedRV, obj.GetResourceVersion()))
	}
	if len(obj.GetFinalizers()) > 0 {
		if obj.GetDeletionTimestamp() == nil {
			previous := obj.DeepCopy()
			now := metav1.NewTime(time.Now().UTC())
			obj.SetDeletionTimestamp(&now)
			rv, err := nextObjectResourceVersion(ctx, tx)
			if err != nil {
				return nil, err
			}
			obj.SetResourceVersion(rv)
			if err := saveObjectBody(ctx, tx, key, obj); err != nil {
				return nil, err
			}
			if err := appendObjectEvent(ctx, tx, key, rv, obj, previous, "MODIFIED"); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit %s deletion timestamp: %w", key.Resource, err)
		}
		return obj, nil
	}
	rv, err := nextObjectResourceVersion(ctx, tx)
	if err != nil {
		return nil, err
	}
	obj.SetResourceVersion(rv)
	if _, err := tx.ExecContext(ctx, `DELETE FROM poc_objects WHERE api_group=$1 AND api_version=$2 AND resource=$3 AND namespace=$4 AND name=$5`, key.Group, key.Version, key.Resource, namespace, name); err != nil {
		return nil, fmt.Errorf("delete %s row: %w", key.Resource, err)
	}
	if err := appendObjectEvent(ctx, tx, key, rv, obj, obj.DeepCopy(), "DELETED"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit %s delete: %w", key.Resource, err)
	}
	return obj, nil
}

func (s *Postgres) LatestObjectResourceVersion(ctx context.Context) (string, error) {
	var rv int64
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM poc_object_revision WHERE singleton=TRUE`).Scan(&rv); err != nil {
		return "", fmt.Errorf("read OCM object resourceVersion: %w", err)
	}
	return strconv.FormatInt(rv, 10), nil
}

func (s *Postgres) ObjectEventsAfter(ctx context.Context, key ResourceKey, resourceVersion, namespace string) ([]ObjectEvent, error) {
	rv, err := strconv.ParseInt(resourceVersion, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid resourceVersion %q: %w", resourceVersion, err)
	}
	query := `SELECT resource_version,event_type,body,previous_body FROM poc_object_events
WHERE api_group=$1 AND api_version=$2 AND resource=$3 AND resource_version>$4 ORDER BY resource_version LIMIT 250`
	args := []any{key.Group, key.Version, key.Resource, rv}
	if namespace != "" {
		query = `SELECT resource_version,event_type,body,previous_body FROM poc_object_events
WHERE api_group=$1 AND api_version=$2 AND resource=$3 AND namespace=$4 AND resource_version>$5 ORDER BY resource_version LIMIT 250`
		args = []any{key.Group, key.Version, key.Resource, namespace, rv}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read %s watch events: %w", key.Resource, err)
	}
	defer rows.Close()
	events := []ObjectEvent{}
	for rows.Next() {
		var version int64
		var eventType string
		var body, previous []byte
		if err := rows.Scan(&version, &eventType, &body, &previous); err != nil {
			return nil, fmt.Errorf("read %s watch event: %w", key.Resource, err)
		}
		obj, err := decodeObject(body)
		if err != nil {
			return nil, err
		}
		event := ObjectEvent{ResourceVersion: strconv.FormatInt(version, 10), Type: eventType, Object: obj}
		if len(previous) > 0 {
			event.Previous, err = decodeObject(previous)
			if err != nil {
				return nil, err
			}
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s watch events: %w", key.Resource, err)
	}
	return events, nil
}

func nextObjectResourceVersion(ctx context.Context, tx *sql.Tx) (string, error) {
	var version int64
	if err := tx.QueryRowContext(ctx, `UPDATE poc_object_revision SET value=value+1 WHERE singleton=TRUE RETURNING value`).Scan(&version); err != nil {
		return "", fmt.Errorf("allocate OCM object resourceVersion: %w", err)
	}
	return strconv.FormatInt(version, 10), nil
}

func appendObjectEvent(ctx context.Context, tx *sql.Tx, key ResourceKey, rv string, obj, previous *unstructured.Unstructured, eventType string) error {
	version, err := strconv.ParseInt(rv, 10, 64)
	if err != nil {
		return fmt.Errorf("parse OCM object resourceVersion: %w", err)
	}
	body, err := json.Marshal(obj.Object)
	if err != nil {
		return fmt.Errorf("encode %s event: %w", key.Resource, err)
	}
	var previousBody any
	if previous != nil {
		encoded, err := json.Marshal(previous.Object)
		if err != nil {
			return fmt.Errorf("encode previous %s event: %w", key.Resource, err)
		}
		previousBody = string(encoded)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO poc_object_events
(resource_version,api_group,api_version,resource,namespace,name,event_type,body,previous_body)
VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb)`, version, key.Group, key.Version, key.Resource,
		obj.GetNamespace(), obj.GetName(), eventType, string(body), previousBody)
	if err != nil {
		return fmt.Errorf("persist %s event: %w", key.Resource, err)
	}
	return nil
}

func saveObjectBody(ctx context.Context, tx *sql.Tx, key ResourceKey, obj *unstructured.Unstructured) error {
	body, err := json.Marshal(obj.Object)
	if err != nil {
		return fmt.Errorf("encode %s: %w", key.Resource, err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE poc_objects SET resource_version=$1,body=$2::jsonb
WHERE api_group=$3 AND api_version=$4 AND resource=$5 AND namespace=$6 AND name=$7`, obj.GetResourceVersion(),
		string(body), key.Group, key.Version, key.Resource, obj.GetNamespace(), obj.GetName())
	if err != nil {
		return fmt.Errorf("save %s: %w", key.Resource, err)
	}
	return nil
}

func decodeObject(body []byte) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{Object: map[string]any{}}
	if err := json.Unmarshal(body, &obj.Object); err != nil {
		return nil, fmt.Errorf("decode OCM object: %w", err)
	}
	return obj, nil
}

func isObjectUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
