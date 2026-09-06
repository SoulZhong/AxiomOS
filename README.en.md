# AxiomOS

[中文](README.md) | **English**

The AI-native operating system for organizations: goals, tasks, people and agents running together in one system. A highly customizable SaaS that also supports private deployment. Vocabulary lives in `CONTEXT.md`, architecture decisions in `docs/adr/`, the workflow definition spec in `docs/spec/`, and the HTTP/MCP contract in `docs/api.md`.

## Run locally

```
make db      # start PostgreSQL (Docker, port 5439)
make run     # backend on :8080; migrates, seeds built-in task types and demo data on start
make web     # frontend dev server on :3000 (in production the backend serves web/out)
make test    # backend tests; with DATABASE_URL set, database integration tests run too
```

Demo accounts: `zhong@demo.local` / `demo1234` (also `li`, `zhang`, `zhao@demo.local`). The first start prints a demo agent token in the log.

The platform console is at `/admin/`. `make run` creates a platform administrator `admin@axiomos.local` / `admin1234` for local development; for real deployments set the environment variables:

```
PLATFORM_ADMIN_EMAIL=admin@example.com PLATFORM_ADMIN_PASSWORD=your-password make run
```

Other environment variables: `DATABASE_URL`, `ADDR` (default :8080), `WEB_DIR` (default web/out), `PUBLIC_URL` (used to build invitation links, default http://localhost:8080), `ALLOW_ORIGINS`, `SEED_DEMO` (default 1).

`AXIOMOS_ALLOW_PRIVATE_EGRESS=1` lets the server reach internal networks. Addresses an organization fills in itself — the notification webhook receiver, a code platform's API base URL, the outbound proxy — are public-https-only by default: loopback, private, link-local and cloud metadata addresses are refused. Set this variable for a private deployment that points at an internal GitLab or an internal webhook receiver. Cloud metadata addresses (169.254.169.254, metadata.google.internal) are refused no matter what. Local development talks to localhost, so `make run` sets it.

Interface languages: Simplified Chinese and English. Every account can switch in the footer or on the login page; each organization has a default language; agents receive tool descriptions and rejection reasons in their owner's language.

Connecting an agent: see [docs/agent-integration.md](docs/agent-integration.md). The MCP endpoint is `/mcp`, authenticated with `Authorization: Bearer <agent token>`.

## Documentation

- `CONTEXT.md` — the glossary; every user-facing name is defined here (Chinese is canonical, English is a translation of it)
- `docs/adr/` — twenty architecture decisions: multi-tenant isolation, the event log as backbone, agent permissions, cross-role workflows, state types, execution records, plain-language terms, tech stack, the design system, boards and sprints, visibility scope, the configurable-SaaS boundary, role workspaces, milestones, the external directory, agent device authorization, notification delivery, external events and links
- `docs/spec/workflow-definition.md` — task types and workflow definitions
- `docs/api.md` — HTTP API and MCP tool contract
- `web/DESIGN.md` — UI design spec v2 (Linear as the base, plus the Axiom starship theme; sources in `docs/design/sources/`)
- `docs/agent-integration.md` — agent integration guide
- `prototypes/` — throwaway validation prototypes

## Layout

- `cmd/axiomd` — the single backend entry point: HTTP API + MCP + static hosting of the frontend
- `internal/domain` — domain layer: workflow kernel, tasks, execution records, events (pure Go, no database)
- `internal/app` — application services: commands and queries
- `internal/store` — PostgreSQL storage and migrations
- `internal/api` — HTTP interface
- `internal/mcp` — the MCP tool surface for agents
- `web` — Next.js frontend (static export to `web/out`)
- `prototypes` — throwaway validation prototypes
