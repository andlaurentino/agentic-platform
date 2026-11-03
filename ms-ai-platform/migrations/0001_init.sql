-- +goose Up
create table catalog_asset (
    id            text primary key,
    kind          text not null,
    domain        text not null,
    manifest      jsonb not null,
    config_schema jsonb not null,
    lifecycle     text not null,
    synced_at     timestamptz not null default now()
);

create table skill (
    id          text primary key,
    name        text not null,
    description text not null default '',
    content     text not null default '',
    version     int  not null default 1,
    created_at  timestamptz not null default now(),
    updated_at  timestamptz not null default now()
);

create table orchestration (
    id          text primary key,
    name        text not null,
    description text not null default '',
    created_at  timestamptz not null default now()
);

create table draft (
    orchestration_id text primary key references orchestration (id) on delete cascade,
    spec             jsonb not null,
    version          int   not null default 1,
    updated_at       timestamptz not null default now(),
    updated_by_name  text  not null default ''
);

-- +goose Down
drop table draft;
drop table orchestration;
drop table skill;
drop table catalog_asset;
