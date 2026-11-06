-- +goose Up
-- Agents become a library shared by orchestrations; a draft keeps only the ids
-- of the agents it uses, plus how they connect.
create table agent (
    id              text primary key,
    spec            jsonb not null,
    version         int   not null default 1,
    created_at      timestamptz not null default now(),
    updated_at      timestamptz not null default now(),
    updated_by_name text  not null default ''
);

insert into agent (id, spec, updated_by_name)
select a->>'id', a, d.updated_by_name
from draft d, jsonb_array_elements(d.spec->'agents') a
where jsonb_typeof(d.spec->'agents') = 'array' and jsonb_typeof(a) = 'object'
on conflict (id) do nothing;

update draft set spec = jsonb_set(spec, '{agents}', coalesce(
    (select jsonb_agg(a->'id') from jsonb_array_elements(spec->'agents') a where jsonb_typeof(a) = 'object'),
    '[]'::jsonb))
where jsonb_typeof(spec->'agents') = 'array';

-- Finds the orchestrations that use an agent (spec->'agents' ? id).
create index draft_agents on draft using gin ((spec->'agents'));

-- +goose Down
update draft set spec = jsonb_set(spec, '{agents}', coalesce(
    (select jsonb_agg(ag.spec order by e.ord)
     from jsonb_array_elements_text(draft.spec->'agents') with ordinality e(id, ord)
     join agent ag on ag.id = e.id),
    '[]'::jsonb));

drop index draft_agents;
drop table agent;
