# Documentation

status=locked
primary_audience=self_hosting_households
readme_role=short_product_entry_and_quick_start
tone=human_concise_specific

## README
README is for a person deciding/installing Tendo, not for the autonomous agent.

Target structure once V1 is usable:
1. product name + one-sentence purpose
2. screenshot
3. "What Tendo does" with 4-6 concrete bullets
4. quick start (Docker Compose)
5. first login / first household
6. reverse proxy note/link
7. update + backup links
8. documentation/support/contributing links
9. project status/license

Rules:
- keep README short; detailed configuration belongs under `docs/`.
- no architecture dump, internal package list, autonomous-agent details, roadmap essay, or generated changelog in README.
- no marketing filler: avoid "powerful", "seamless", "revolutionary", "robust", "next-generation", "effortlessly".
- short paragraphs, concrete headings, exact commands.
- never claim unimplemented features.
- screenshots reflect current release.
- instructions state prerequisites first and use copy-pasteable verified commands.
- README must be understandable by a non-developer comfortable with Docker/self-hosting.

## Documentation tree
docs/user/
- using Tendo
- subjects/items/repeat toggle/history
- invitations/accounts

docs/admin/
- installation
- configuration reference
- reverse proxy
- OIDC/PocketID
- updates
- backup + restore
- troubleshooting

docs/development/
- local development
- API/OpenAPI
- tests
- release process

docs/adr/
- durable engineering decisions only

## Reverse proxy documentation
Provide maintained examples for:
- Caddy
- Nginx
- Traefik

Clearly state:
- reverse proxy terminates HTTPS
- Tendo backend is HTTP
- public URL setting
- trusted proxy configuration
- forwarded headers
- root-path/subdomain expectation
- secure cookie/OIDC considerations

Never tell users to expose PostgreSQL publicly.

## Config documentation
- one canonical configuration reference
- every environment variable: name, required/default, example, security note
- sample `.env.example` contains no secrets and is kept synchronized/testable where practical

## Writing quality
- documentation changes ship with behavior changes
- no duplicate contradictory instructions across README/docs
- prefer task-oriented docs ("Back up Tendo") over component prose
- troubleshooting: observable symptom -> checks -> fix
- verify commands/examples when feasible
