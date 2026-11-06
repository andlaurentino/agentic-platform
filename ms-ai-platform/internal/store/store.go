// Package store is the Postgres persistence layer.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	// ErrVersionConflict means the draft or agent changed since the caller read it.
	ErrVersionConflict = errors.New("version conflict")
	// ErrInUse means an agent cannot be deleted because orchestrations use it.
	ErrInUse = errors.New("in use")
	// ErrUnknownAgent means a draft uses an agent that is not in the library.
	ErrUnknownAgent = errors.New("unknown agent")
)

type Store struct {
	pool *pgxpool.Pool
	// UpgradeSpec, when set, rewrites outdated specs as drafts and agents are read.
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

// KnownSkills reports which skill ids exist, for validation.
func (s *Store) KnownSkills(ctx context.Context) (func(id string) bool, error) {
	skills, err := s.ListSkills(ctx)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]bool, len(skills))
	for _, k := range skills {
		ids[k.ID] = true
	}
	return func(id string) bool { return ids[id] }, nil
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

// Agent is a library agent: reusable by any number of orchestrations, with its
// own version for optimistic concurrency.
type Agent struct {
	spec.Agent
	Version       int       `json:"version"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	UpdatedByName string    `json:"updatedByName"`
	// UsedBy lists the orchestrations whose draft uses the agent.
	UsedBy []OrchestrationRef `json:"usedBy"`
}

type OrchestrationRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

const agentSelect = `select a.id, a.spec, a.version, a.created_at, a.updated_at, a.updated_by_name,
	coalesce((select jsonb_agg(jsonb_build_object('id', o.id, 'name', o.name) order by o.name)
		from draft d join orchestration o on o.id = d.orchestration_id
		where d.spec->'agents' ? a.id), '[]'::jsonb)
	from agent a`

func (s *Store) scanAgent(row pgx.Row) (Agent, error) {
	var a Agent
	var id string
	err := row.Scan(&id, &a.Agent, &a.Version, &a.CreatedAt, &a.UpdatedAt, &a.UpdatedByName, &a.UsedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	a.ID = id
	a.Normalize()
	if s.UpgradeSpec != nil {
		o := spec.Orchestration{Agents: []spec.Agent{a.Agent}}
		s.UpgradeSpec(&o)
		a.Agent = o.Agents[0]
	}
	return a, err
}

func (s *Store) queryAgents(ctx context.Context, sql string, args ...any) ([]Agent, error) {
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Agent{}
	for rows.Next() {
		a, err := s.scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) ListAgents(ctx context.Context) ([]Agent, error) {
	return s.queryAgents(ctx, agentSelect+` order by lower(a.spec->>'name'), a.id`)
}

func (s *Store) GetAgent(ctx context.Context, id string) (Agent, error) {
	return s.scanAgent(s.pool.QueryRow(ctx, agentSelect+` where a.id = $1`, id))
}

// CreateAgent adds an agent to the library under a.ID.
func (s *Store) CreateAgent(ctx context.Context, a spec.Agent, by string) (Agent, error) {
	a.Normalize()
	if _, err := s.pool.Exec(ctx, `insert into agent (id, spec, updated_by_name) values ($1, $2, $3)`, a.ID, a, by); err != nil {
		return Agent{}, err
	}
	return s.GetAgent(ctx, a.ID)
}

// SaveAgent writes the agent only if the stored version still equals expectedVersion.
// On a mismatch it returns the current agent and ErrVersionConflict.
func (s *Store) SaveAgent(ctx context.Context, id string, expectedVersion int, a spec.Agent, by string) (Agent, error) {
	a.ID = id
	a.Normalize()
	tag, err := s.pool.Exec(ctx, `
		update agent set spec = $3, version = version + 1, updated_at = now(), updated_by_name = $4
		where id = $1 and version = $2`, id, expectedVersion, a, by)
	if err != nil {
		return Agent{}, err
	}
	current, err := s.GetAgent(ctx, id)
	if err == nil && tag.RowsAffected() == 0 {
		err = ErrVersionConflict
	}
	return current, err
}

// DeleteAgent removes an agent no orchestration uses. Otherwise it returns
// ErrInUse and the orchestrations that use it.
func (s *Store) DeleteAgent(ctx context.Context, id string) ([]OrchestrationRef, error) {
	tag, err := s.pool.Exec(ctx, `delete from agent where id = $1 and not exists (select 1 from draft where spec->'agents' ? $1)`, id)
	if err != nil || tag.RowsAffected() > 0 {
		return nil, err
	}
	a, err := s.GetAgent(ctx, id)
	if err != nil {
		return nil, err
	}
	return a.UsedBy, ErrInUse
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
	OrchestrationID string     `json:"orchestrationId"`
	Spec            spec.Graph `json:"spec"`
	Version         int        `json:"version"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	UpdatedByName   string     `json:"updatedByName"`
	// Agents are the library agents the graph uses, in graph order.
	Agents []Agent `json:"agents"`
}

const orchSelect = `select o.id, o.name, o.description, o.created_at, d.updated_at,
	coalesce((select jsonb_agg(jsonb_build_object('id', a.id, 'name', a.spec->>'name',
			'description', coalesce(a.spec->>'description', ''), 'root', a.id = d.spec->>'rootAgent') order by e.ord)
		from jsonb_array_elements_text(d.spec->'agents') with ordinality e(id, ord)
		join agent a on a.id = e.id), '[]'::jsonb)
	from orchestration o join draft d on d.orchestration_id = o.id`

func scanOrchestration(row pgx.Row) (Orchestration, error) {
	var o Orchestration
	err := row.Scan(&o.ID, &o.Name, &o.Description, &o.CreatedAt, &o.UpdatedAt, &o.Agents)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
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

// CreateOrchestration inserts the orchestration, its starter agent and its draft.
func (s *Store) CreateOrchestration(ctx context.Context, o Orchestration, g spec.Graph, starter spec.Agent) (Orchestration, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `insert into orchestration (id, name, description) values ($1, $2, $3)`, o.ID, o.Name, o.Description); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into agent (id, spec) values ($1, $2)`, starter.ID, starter); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into draft (orchestration_id, spec) values ($1, $2)`, o.ID, g)
		return err
	})
	if err != nil {
		return o, err
	}
	return s.GetOrchestration(ctx, o.ID)
}

// DeleteOrchestration deletes the orchestration; its agents stay in the library.
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
	if err != nil {
		return d, err
	}
	if s.UpgradeSpec != nil {
		o := spec.Orchestration{Guardrails: d.Spec.Guardrails}
		s.UpgradeSpec(&o)
		d.Spec.Guardrails = o.Guardrails
	}
	agents, err := s.queryAgents(ctx, agentSelect+` where a.id = any($1)`, d.Spec.Agents)
	if err != nil {
		return d, err
	}
	byID := make(map[string]Agent, len(agents))
	for _, a := range agents {
		byID[a.ID] = a
	}
	d.Agents = []Agent{}
	for _, id := range d.Spec.Agents {
		if a, ok := byID[id]; ok {
			d.Agents = append(d.Agents, a)
		}
	}
	return d, nil
}

// ResolveDraft returns the draft with its agents taken from the library, ready to validate or run.
func (s *Store) ResolveDraft(ctx context.Context, orchestrationID string) (spec.Orchestration, error) {
	d, err := s.GetDraft(ctx, orchestrationID)
	if err != nil {
		return spec.Orchestration{}, err
	}
	library := make(map[string]spec.Agent, len(d.Agents))
	for _, a := range d.Agents {
		library[a.ID] = a.Agent
	}
	return d.Spec.Resolve(library), nil
}

// SaveDraft writes the graph only if the stored version still equals expectedVersion
// and every agent it uses exists. On a version mismatch it returns the current
// draft and ErrVersionConflict.
func (s *Store) SaveDraft(ctx context.Context, orchestrationID string, expectedVersion int, g spec.Graph, by string) (Draft, error) {
	g.Normalize()
	var saved bool
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var missing []string
		rows, err := tx.Query(ctx, `select x from unnest($1::text[]) x where not exists (select 1 from agent where id = x)`, g.Agents)
		if err != nil {
			return err
		}
		if missing, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
			return err
		}
		if len(missing) > 0 {
			return fmt.Errorf("%w: %s", ErrUnknownAgent, strings.Join(missing, ", "))
		}
		tag, err := tx.Exec(ctx, `
			update draft set spec = $3, version = version + 1, updated_at = now(), updated_by_name = $4
			where orchestration_id = $1 and version = $2`, orchestrationID, expectedVersion, g, by)
		saved = tag.RowsAffected() > 0
		return err
	})
	if err != nil {
		return Draft{}, err
	}
	current, err := s.GetDraft(ctx, orchestrationID)
	if err == nil && !saved {
		err = ErrVersionConflict
	}
	return current, err
}
