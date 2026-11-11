# nerdui — Technical Specification

- **Status:** Draft
- **Version:** 0.1
- **Last updated:** YYYY-MM-DD
- **Owner:**

## 1. Purpose and goals

<!-- Why nerdui exists, what problem it solves, success criteria. -->

## 2. Users and roles

| Role | Description | Permissions |
| ---- | ----------- | ----------- |
|      |             |             |

## 3. Scope

### 3.1 In scope

### 3.2 Out of scope

## 4. Environment and constraints

- Target OS / architectures: Linux amd64, arm64
- containerd versions:
- nerdctl versions:
- Supported browsers:
- Deployment: binary on host / container image (see [deployment](../deployment.md))
- Architecture: [ADR-0001](../adr/0001-go-containerd-socket.md)

## 5. Functional requirements

<!-- Short list; details in features/*.md -->

| ID         | Area       | Requirement | Priority | Feature doc |
| ---------- | ---------- | ----------- | -------- | ----------- |
| FR-CTR-001 | Containers |             | Must     |             |
| FR-IMG-001 | Images     |             |          |             |
| FR-VOL-001 | Volumes    |             |          |             |
| FR-NET-001 | Networks   |             |          |             |
| FR-NS-001  | Namespaces |             |          |             |
| FR-AUTH-001| Auth       |             |          |             |

## 6. Non-functional requirements

| ID      | Category        | Requirement | Target / metric |
| ------- | --------------- | ----------- | --------------- |
| NFR-001 | Performance     |             |                 |
| NFR-002 | Scalability     | Max containers/images on a host |  |
| NFR-003 | Security        | Authentication, TLS, audit log |  |
| NFR-004 | Reliability     |             |                 |
| NFR-005 | Compatibility   | Objects manageable with nerdctl CLI |  |
| NFR-006 | Observability   | Logs, metrics, health checks |  |
| NFR-007 | Usability / a11y|             |                 |
| NFR-008 | Resource usage  | Memory/CPU of nerdui itself |  |

## 7. API

Contract: [`api/openapi.yaml`](../../api/openapi.yaml).

## 8. UI / UX

<!-- Screens, navigation, mockups (links). -->

## 9. Security

<!-- Threat model, authn/authz, secrets, audit. See SECURITY.md. -->

## 10. Acceptance criteria

## 11. Milestones

| Milestone | Scope | Target date |
| --------- | ----- | ----------- |
| MVP       |       |             |

## 12. Open questions

-

## 13. Glossary

| Term      | Definition |
| --------- | ---------- |
| Namespace | containerd namespace (`default`, `k8s.io`, …) isolating containers/images |
