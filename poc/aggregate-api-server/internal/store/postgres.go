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
	_ "github.com/jackc/pgx/v5/stdlib"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	workv1 "open-cluster-management.io/api/work/v1"
)

var resource = schema.GroupResource{Group: "work.open-cluster-management.io", Resource: "manifestworks"}

type Postgres struct {
	db *sql.DB
}

func OpenPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL connection: %w", err)
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	store := &Postgres{db: db}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.migrateObjects(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Postgres) Close() error { return s.db.Close() }

func (s *Postgres) migrate(ctx context.Context) error {
	const migration = `
CREATE TABLE IF NOT EXISTS manifestwork_revision (
  singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
  value BIGINT NOT NULL DEFAULT 0
);
INSERT INTO manifestwork_revision(singleton, value) VALUES (TRUE, 0)
  ON CONFLICT (singleton) DO NOTHING;
CREATE TABLE IF NOT EXISTS manifestworks (
  namespace TEXT NOT NULL,
  name TEXT NOT NULL,
  resource_version BIGINT NOT NULL,
  body JSONB NOT NULL,
  PRIMARY KEY (namespace, name)
);
CREATE TABLE IF NOT EXISTS manifestwork_events (
  resource_version BIGINT PRIMARY KEY,
  namespace TEXT NOT NULL,
  name TEXT NOT NULL,
  event_type TEXT NOT NULL,
  body JSONB NOT NULL,
  previous_body JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE manifestwork_events ADD COLUMN IF NOT EXISTS previous_body JSONB;
CREATE INDEX IF NOT EXISTS manifestwork_events_namespace_rv
  ON manifestwork_events (namespace, resource_version);
`
	for _, statement := range strings.Split(migration, ";") {
		if statement = strings.TrimSpace(statement); statement != "" {
			if _, err := s.db.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("initialize ManifestWork tables: %w", err)
			}
		}
	}
	return nil
}

func (s *Postgres) Get(ctx context.Context, namespace, name string) (*workv1.ManifestWork, error) {
	var body []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT body FROM manifestworks WHERE namespace = $1 AND name = $2`, namespace, name).Scan(&body)
	if err == sql.ErrNoRows {
		return nil, apierrors.NewNotFound(resource, name)
	}
	if err != nil {
		return nil, fmt.Errorf("read ManifestWork %s/%s: %w", namespace, name, err)
	}
	return decode(body)
}

func (s *Postgres) List(ctx context.Context, namespace string) ([]workv1.ManifestWork, string, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, "", fmt.Errorf("begin ManifestWork list snapshot: %w", err)
	}
	defer tx.Rollback()
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT value FROM manifestwork_revision WHERE singleton = TRUE`).Scan(&revision); err != nil {
		return nil, "", fmt.Errorf("read ManifestWork list resourceVersion: %w", err)
	}
	query := `SELECT body FROM manifestworks ORDER BY namespace, name`
	args := []any{}
	if namespace != "" {
		query = `SELECT body FROM manifestworks WHERE namespace = $1 ORDER BY namespace, name`
		args = append(args, namespace)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list ManifestWorks: %w", err)
	}
	defer rows.Close()
	items := []workv1.ManifestWork{}
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, "", fmt.Errorf("read ManifestWork row: %w", err)
		}
		item, err := decode(body)
		if err != nil {
			return nil, "", err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate ManifestWork rows: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, "", fmt.Errorf("finish ManifestWork list snapshot: %w", err)
	}
	return items, strconv.FormatInt(revision, 10), nil
}

func (s *Postgres) Create(ctx context.Context, input *workv1.ManifestWork) (*workv1.ManifestWork, error) {
	obj := input.DeepCopy()
	obj.UID = types.UID(uuid.NewUUID())
	obj.CreationTimestamp = metav1.Now()
	obj.DeletionTimestamp = nil
	obj.DeletionGracePeriodSeconds = nil
	obj.ResourceVersion = ""
	obj.Generation = 1
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin ManifestWork create: %w", err)
	}
	defer tx.Rollback()
	rv, err := nextResourceVersion(ctx, tx)
	if err != nil {
		return nil, err
	}
	obj.ResourceVersion = rv
	body, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("encode ManifestWork: %w", err)
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO manifestworks(namespace, name, resource_version, body) VALUES ($1, $2, $3, $4::jsonb)`,
		obj.Namespace, obj.Name, rv, string(body))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, apierrors.NewAlreadyExists(resource, obj.Name)
		}
		return nil, fmt.Errorf("insert ManifestWork: %w", err)
	}
	if err := appendEvent(ctx, tx, rv, obj, "ADDED", nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit ManifestWork create: %w", err)
	}
	return obj, nil
}

func (s *Postgres) Update(ctx context.Context, input *workv1.ManifestWork, statusOnly bool) (*workv1.ManifestWork, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin ManifestWork update: %w", err)
	}
	defer tx.Rollback()
	var existingBody []byte
	err = tx.QueryRowContext(ctx,
		`SELECT body FROM manifestworks WHERE namespace = $1 AND name = $2 FOR UPDATE`,
		input.Namespace, input.Name).Scan(&existingBody)
	if err == sql.ErrNoRows {
		return nil, apierrors.NewNotFound(resource, input.Name)
	}
	if err != nil {
		return nil, fmt.Errorf("read ManifestWork for update: %w", err)
	}
	existing, err := decode(existingBody)
	if err != nil {
		return nil, err
	}
	if input.ResourceVersion == "" || input.ResourceVersion != existing.ResourceVersion {
		return nil, apierrors.NewConflict(resource, input.Name, fmt.Errorf("resourceVersion %q is stale; current value is %q", input.ResourceVersion, existing.ResourceVersion))
	}
	updated := input.DeepCopy()
	updated.UID = existing.UID
	updated.CreationTimestamp = existing.CreationTimestamp
	updated.DeletionTimestamp = existing.DeletionTimestamp
	updated.DeletionGracePeriodSeconds = existing.DeletionGracePeriodSeconds
	if statusOnly {
		updated.Spec = existing.Spec
		updated.ObjectMeta = *existing.ObjectMeta.DeepCopy()
		updated.Status = input.Status
	} else {
		updated.Status = existing.Status
		if !equalJSON(input.Spec, existing.Spec) {
			updated.Generation = existing.Generation + 1
		} else {
			updated.Generation = existing.Generation
		}
	}
	rv, err := nextResourceVersion(ctx, tx)
	if err != nil {
		return nil, err
	}
	updated.ResourceVersion = rv
	body, err := json.Marshal(updated)
	if err != nil {
		return nil, fmt.Errorf("encode updated ManifestWork: %w", err)
	}
	eventType := "MODIFIED"
	if !updated.DeletionTimestamp.IsZero() && len(updated.Finalizers) == 0 {
		_, err = tx.ExecContext(ctx, `DELETE FROM manifestworks WHERE namespace = $1 AND name = $2`, updated.Namespace, updated.Name)
		if err != nil {
			return nil, fmt.Errorf("complete finalized ManifestWork deletion: %w", err)
		}
		eventType = "DELETED"
	} else {
		_, err = tx.ExecContext(ctx,
			`UPDATE manifestworks SET resource_version = $1, body = $2::jsonb WHERE namespace = $3 AND name = $4`,
			rv, string(body), updated.Namespace, updated.Name)
		if err != nil {
			return nil, fmt.Errorf("update ManifestWork row: %w", err)
		}
	}
	if err := appendEvent(ctx, tx, rv, updated, eventType, existing); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit ManifestWork update: %w", err)
	}
	return updated, nil
}

func (s *Postgres) Delete(ctx context.Context, namespace, name, expectedRV string) (*workv1.ManifestWork, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin ManifestWork delete: %w", err)
	}
	defer tx.Rollback()
	var body []byte
	err = tx.QueryRowContext(ctx,
		`SELECT body FROM manifestworks WHERE namespace = $1 AND name = $2 FOR UPDATE`, namespace, name).Scan(&body)
	if err == sql.ErrNoRows {
		return nil, apierrors.NewNotFound(resource, name)
	}
	if err != nil {
		return nil, fmt.Errorf("read ManifestWork for delete: %w", err)
	}
	obj, err := decode(body)
	if err != nil {
		return nil, err
	}
	if expectedRV != "" && expectedRV != obj.ResourceVersion {
		return nil, apierrors.NewConflict(resource, name, fmt.Errorf("resourceVersion %q is stale; current value is %q", expectedRV, obj.ResourceVersion))
	}
	if len(obj.Finalizers) > 0 {
		if obj.DeletionTimestamp.IsZero() {
			previous := obj.DeepCopy()
			now := metav1.Now()
			obj.DeletionTimestamp = &now
			rv, err := nextResourceVersion(ctx, tx)
			if err != nil {
				return nil, err
			}
			obj.ResourceVersion = rv
			updated, _ := json.Marshal(obj)
			if _, err := tx.ExecContext(ctx,
				`UPDATE manifestworks SET resource_version = $1, body = $2::jsonb WHERE namespace = $3 AND name = $4`,
				rv, string(updated), namespace, name); err != nil {
				return nil, fmt.Errorf("mark ManifestWork for deletion: %w", err)
			}
			if err := appendEvent(ctx, tx, rv, obj, "MODIFIED", previous); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit ManifestWork deletion timestamp: %w", err)
		}
		return obj, nil
	}
	rv, err := nextResourceVersion(ctx, tx)
	if err != nil {
		return nil, err
	}
	obj.ResourceVersion = rv
	if _, err := tx.ExecContext(ctx, `DELETE FROM manifestworks WHERE namespace = $1 AND name = $2`, namespace, name); err != nil {
		return nil, fmt.Errorf("delete ManifestWork row: %w", err)
	}
	if err := appendEvent(ctx, tx, rv, obj, "DELETED", obj); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit ManifestWork deletion: %w", err)
	}
	return obj, nil
}

func (s *Postgres) LatestResourceVersion(ctx context.Context) (string, error) {
	var rv int64
	err := s.db.QueryRowContext(ctx, `SELECT value FROM manifestwork_revision WHERE singleton = TRUE`).Scan(&rv)
	if err != nil {
		return "", fmt.Errorf("read latest ManifestWork resourceVersion: %w", err)
	}
	return strconv.FormatInt(rv, 10), nil
}

func (s *Postgres) EventsAfter(ctx context.Context, resourceVersion, namespace string) ([]Event, error) {
	rv, err := strconv.ParseInt(resourceVersion, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid resourceVersion %q: %w", resourceVersion, err)
	}
	query := `SELECT resource_version, event_type, body, previous_body FROM manifestwork_events WHERE resource_version > $1 ORDER BY resource_version LIMIT 250`
	args := []any{rv}
	if namespace != "" {
		query = `SELECT resource_version, event_type, body, previous_body FROM manifestwork_events WHERE resource_version > $1 AND namespace = $2 ORDER BY resource_version LIMIT 250`
		args = append(args, namespace)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read ManifestWork watch events: %w", err)
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var rv int64
		var eventType string
		var body []byte
		var previousBody []byte
		if err := rows.Scan(&rv, &eventType, &body, &previousBody); err != nil {
			return nil, fmt.Errorf("read ManifestWork watch event: %w", err)
		}
		obj, err := decode(body)
		if err != nil {
			return nil, err
		}
		event := Event{ResourceVersion: strconv.FormatInt(rv, 10), Type: eventType, Object: obj}
		if len(previousBody) > 0 {
			event.Previous, err = decode(previousBody)
			if err != nil {
				return nil, err
			}
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ManifestWork watch events: %w", err)
	}
	return events, nil
}

func nextResourceVersion(ctx context.Context, tx *sql.Tx) (string, error) {
	var rv int64
	if err := tx.QueryRowContext(ctx, `UPDATE manifestwork_revision SET value = value + 1 WHERE singleton = TRUE RETURNING value`).Scan(&rv); err != nil {
		return "", fmt.Errorf("allocate ManifestWork resourceVersion: %w", err)
	}
	return strconv.FormatInt(rv, 10), nil
}

func appendEvent(ctx context.Context, tx *sql.Tx, rv string, obj *workv1.ManifestWork, eventType string, previous *workv1.ManifestWork) error {
	version, err := strconv.ParseInt(rv, 10, 64)
	if err != nil {
		return fmt.Errorf("parse ManifestWork resourceVersion: %w", err)
	}
	body, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("encode ManifestWork event: %w", err)
	}
	var previousJSON any
	if previous != nil {
		previousBody, err := json.Marshal(previous)
		if err != nil {
			return fmt.Errorf("encode previous ManifestWork event object: %w", err)
		}
		previousJSON = string(previousBody)
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO manifestwork_events(resource_version, namespace, name, event_type, body, previous_body) VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb)`,
		version, obj.Namespace, obj.Name, eventType, string(body), previousJSON)
	if err != nil {
		return fmt.Errorf("persist ManifestWork event: %w", err)
	}
	return nil
}

func decode(body []byte) (*workv1.ManifestWork, error) {
	obj := &workv1.ManifestWork{}
	if err := json.Unmarshal(body, obj); err != nil {
		return nil, fmt.Errorf("decode stored ManifestWork: %w", err)
	}
	return obj, nil
}

func equalJSON(a, b any) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	return err == nil && string(left) == string(right)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
