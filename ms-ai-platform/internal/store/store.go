// Package store is the Postgres persistence layer.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"agentic-platform/lib-agent-assets/pkg/assets"
	"agentic-platform/ms-ai-platform/internal/spec"
	"agentic-platform/ms-ai-platform/migrations"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrVersionConflict means the draft changed since the caller read it.
	ErrVersionConflict = errors.New("version conflict")
)

type Store struct {
	pool *pgxpool.Pool
	// UpgradeSpec, when set, rewrites outdated specs as drafts are read.
	UpgradeSpec func(*spec.Orchestration)
}

// Open connects and runs migrations.
func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return nil, err
	}
	if err := goose.UpContext(ctx, db, "."); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close()                         { s.pool.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// SyncCatalog upserts every manifest and removes rows for assets that no longer exist.
func (s *Store) SyncCatalog(ctx context.Context, ms []assets.Manifest) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		ids := make([]string, 0, len(ms))
		for _, m := range ms {
			manifest, err := json.Marshal(m)
			if err != nil {
				return err
			}
			schema, err := json.Marshal(m.ConfigSchema)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `
				insert into catalog_asset (id, kind, domain, manifest, config_schema, lifecycle, synced_at)
				values ($1, $2, $3, $4, $5, $6, now())
				on conflict (id) do update set kind = excluded.kind, domain = excluded.domain, manifest = excluded.manifest,
					config_schema = excluded.config_schema, lifecycle = excluded.lifecycle, synced_at = now()`,
				m.ID, m.Kind, m.Domain, manifest, schema, m.Lifecycle)
			if err != nil {
				return err
			}
			ids = append(ids, m.ID)
		}
		_, err := tx.Exec(ctx, `delete from catalog_asset where not (id = any($1))`, ids)
		return err
	})
}

// Skill is a reusable block of instructions.
type Skill struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Content     string    `json:"content"`
	Version     int       `json:"version"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

const skillCols = `id, name, description, content, version, created_at, updated_at`

func scanSkill(row pgx.Row) (Skill, error) {
	var k Skill
	err := row.Scan(&k.ID, &k.Name, &k.Description, &k.Content, &k.Version, &k.CreatedAt, &k.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return k, ErrNotFound
	}
	return k, err
}

func (s *Store) ListSkills(ctx context.Context) ([]Skill, error) {
	rows, err := s.pool.Query(ctx, `select `+skillCols+` from skill order by name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Skill{}
	for rows.Next() {
		k, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) GetSkill(ctx context.Context, id string) (Skill, error) {
	return scanSkill(s.pool.QueryRow(ctx, `select `+skillCols+` from skill where id = $1`, id))
}

func (s *Store) CreateSkill(ctx context.Context, k Skill) (Skill, error) {
	return scanSkill(s.pool.QueryRow(ctx,
		`insert into skill (id, name, description, content) values ($1, $2, $3, $4) returning `+skillCols,
		k.ID, k.Name, k.Description, k.Content))
}

func (s *Store) UpdateSkill(ctx context.Context, k Skill) (Skill, error) {
	return scanSkill(s.pool.QueryRow(ctx,
		`update skill set name = $2, description = $3, content = $4, version = version + 1, updated_at = now()
		 where id = $1 returning `+skillCols,
		k.ID, k.Name, k.Description, k.Content))
}

func (s *Store) DeleteSkill(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `delete from skill where id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// Orchestration is the header row; its content lives in the draft.
type Orchestration struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// Agents is a summary of the draft's agents, for lists.
	Agents []AgentSummary `json:"agents"`
}

type AgentSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Root        bool   `json:"root"`
}

// Draft is the single shared working copy of an orchestration.
type Draft struct {
	OrchestrationID string             `json:"orchestrationId"`
	Spec            spec.Orchestration `json:"spec"`
	Version         int                `json:"version"`
	UpdatedAt       time.Time          `json:"updatedAt"`
	UpdatedByName   string             `json:"updatedByName"`
}

func summarize(o *Orchestration, sp spec.Orchestration) {
	o.Agents = []AgentSummary{}
	for _, a := range sp.Agents {
		o.Agents = append(o.Agents, AgentSummary{ID: a.ID, Name: a.Name, Description: a.Description, Root: a.ID == sp.RootAgent})
	}
}

const orchSelect = `select o.id, o.name, o.description, o.created_at, d.updated_at, d.spec
	from orchestration o join draft d on d.orchestration_id = o.id`

func scanOrchestration(row pgx.Row) (Orchestration, error) {
	var o Orchestration
	var sp spec.Orchestration
	err := row.Scan(&o.ID, &o.Name, &o.Description, &o.CreatedAt, &o.UpdatedAt, &sp)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	summarize(&o, sp)
	return o, err
}

func (s *Store) ListOrchestrations(ctx context.Context) ([]Orchestration, error) {
	rows, err := s.pool.Query(ctx, orchSelect+` order by d.updated_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Orchestration{}
	for rows.Next() {
		o, err := scanOrchestration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) GetOrchestration(ctx context.Context, id string) (Orchestration, error) {
	return scanOrchestration(s.pool.QueryRow(ctx, orchSelect+` where o.id = $1`, id))
}

// CreateOrchestration inserts the orchestration and its starter draft.
func (s *Store) CreateOrchestration(ctx context.Context, o Orchestration, starter spec.Orchestration) (Orchestration, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `insert into orchestration (id, name, description) values ($1, $2, $3)`, o.ID, o.Name, o.Description); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into draft (orchestration_id, spec) values ($1, $2)`, o.ID, starter)
		return err
	})
	if err != nil {
		return o, err
	}
	return s.GetOrchestration(ctx, o.ID)
}

func (s *Store) DeleteOrchestration(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `delete from orchestration where id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

const draftCols = `orchestration_id, spec, version, updated_at, updated_by_name`

func scanDraft(row pgx.Row) (Draft, error) {
	var d Draft
	err := row.Scan(&d.OrchestrationID, &d.Spec, &d.Version, &d.UpdatedAt, &d.UpdatedByName)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	d.Spec.Normalize()
	return d, err
}

func (s *Store) GetDraft(ctx context.Context, orchestrationID string) (Draft, error) {
	d, err := scanDraft(s.pool.QueryRow(ctx, `select `+draftCols+` from draft where orchestration_id = $1`, orchestrationID))
	if err == nil && s.UpgradeSpec != nil {
		s.UpgradeSpec(&d.Spec)
	}
	return d, err
}

// SaveDraft writes the spec only if the stored version still equals expectedVersion.
// On a mismatch it returns the current draft and ErrVersionConflict.
func (s *Store) SaveDraft(ctx context.Context, orchestrationID string, expectedVersion int, sp spec.Orchestration, by string) (Draft, error) {
	d, err := scanDraft(s.pool.QueryRow(ctx, `
		update draft set spec = $3, version = version + 1, updated_at = now(), updated_by_name = $4
		where orchestration_id = $1 and version = $2
		returning `+draftCols, orchestrationID, expectedVersion, sp, by))
	if !errors.Is(err, ErrNotFound) {
		return d, err
	}
	current, err := s.GetDraft(ctx, orchestrationID)
	if err != nil {
		return current, err
	}
	return current, ErrVersionConflict
}
