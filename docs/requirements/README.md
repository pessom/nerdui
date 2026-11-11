# Requirements

| Document                                                       | Purpose                                     |
| -------------------------------------------------------------- | ------------------------------------------- |
| [technical-specification.md](technical-specification.md)       | Technical specification (scope, NFRs, constraints) |
| [features/](features/)                                         | One file per feature, from [feature template](features/_template.md) |

## Conventions

- Requirement IDs: `FR-<area>-<n>` (functional), `NFR-<n>` (non-functional),
  e.g. `FR-CTR-001`, `NFR-003`.
- Priority: **Must / Should / Could / Won't** (MoSCoW).
- Each feature file links to its GitHub issue(s); PRs reference requirement IDs.
- Status: `Draft` → `Approved` → `Implemented`.
