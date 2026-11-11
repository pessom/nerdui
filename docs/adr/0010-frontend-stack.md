# 0010. Frontend stack

- **Status:** Accepted
- **Date:** 2026-11-11

## Context

The UI is an admin-style SPA embedded into the Go binary: many tables with filters and sorting,
forms, live streams, a code editor for compose files, a terminal, light/dark themes (#90).

## Options considered

- **Mantine + TanStack** — rich ready-made components for admin UIs, built-in dark mode.
- **shadcn/ui + Tailwind + TanStack** — flexible, but components are copied into the project and
  maintained by us.
- **MUI + React Router** — mature but heavy; advanced DataGrid features are paid.

## Decision

| Concern | Choice |
| --- | --- |
| Components, theming | Mantine (color scheme `auto` / `light` / `dark`) |
| Server state | TanStack Query; invalidated by WebSocket events (ADR-0007) |
| Routing | TanStack Router (type-safe routes; environment in the URL: `/env/:env/...`) |
| Tables | TanStack Table (sorting, filtering, column visibility) |
| API client | `openapi-fetch` + types generated from `api/openapi.yaml` |
| Code editor | CodeMirror 6 with YAML / dotenv modes (#26) |
| Terminal | xterm.js (#15) |
| Tests | Vitest + Testing Library |

- UI language: English for MVP; strings are kept in components, i18n is a later item.
- User preferences (theme, visible columns) are stored in `localStorage` in MVP and moved to
  per-user settings in the database later.
- Bundle budget: initial JS ≤ 500 KB gzip; CodeMirror and xterm.js are lazy-loaded.

## Consequences

- Fast delivery of standard admin screens; consistent look in light and dark modes.
- Mantine major upgrades need to be tracked (Dependabot group).
