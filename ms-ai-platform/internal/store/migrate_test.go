package store

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"agentic-platform/ms-ai-platform/internal/httpx"
	"agentic-platform/ms-ai-platform/migrations"
)

// A draft saved before the agent library had its agents embedded: migrating
// moves them into the library, and migrating down puts them back.
// Needs a Postgres server: DATABASE_URL_TEST (a scratch database is created and dropped).
func TestAgentLibraryMigration(t *testing.T) {
	base := os.Getenv("DATABASE_URL_TEST")
	if base == "" {
		t.Skip("DATABASE_URL_TEST not set")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	name := strings.ToLower(httpx.NewID("migrate_test"))
	if _, err := admin.Exec(ctx, "create database "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "drop database "+name+" with (force)") })

	u, _ := url.Parse(base)
	u.Path = "/" + name
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, db, ".", 1); err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `
		insert into orchestration (id, name) values ('orc_1', 'Care');
		insert into draft (orchestration_id, spec, updated_by_name) values ('orc_1', '{
			"rootAgent": "agt_root",
			"agents": [
				{"id": "agt_root", "name": "Router", "model": {"asset": "core.llm.gateway"}},
				{"id": "agt_weather", "name": "Weather", "model": {"asset": "core.llm.gateway"}}
			],
			"edges": [{"from": "agt_root", "to": "agt_weather", "mode": "transfer"}],
			"layout": {}
		}', 'Ana')`)
	if err != nil {
		t.Fatal(err)
	}

	if err := goose.UpToContext(ctx, db, ".", 2); err != nil {
		t.Fatal(err)
	}
	var agents, names string
	if err := db.QueryRowContext(ctx, `select spec->>'agents' from draft`).Scan(&agents); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `select string_agg(spec->>'name' || '/' || updated_by_name, ',' order by id) from agent`).Scan(&names); err != nil {
		t.Fatal(err)
	}
	if agents != `["agt_root", "agt_weather"]` || names != "Router/Ana,Weather/Ana" {
		t.Fatalf("after up: draft agents %s, library %s", agents, names)
	}

	if err := goose.DownToContext(ctx, db, ".", 1); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `select jsonb_path_query_array(spec, '$.agents[*].name')::text from draft`).Scan(&agents); err != nil {
		t.Fatal(err)
	}
	if agents != `["Router", "Weather"]` {
		t.Fatalf("after down: %s", agents)
	}
}
