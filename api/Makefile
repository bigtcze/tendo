OAPI_CODEGEN_VERSION := v2.4.1
GO_IMAGE := golang:1.26.8-bookworm

.PHONY: generate validate check

generate:
	mkdir -p generated
	docker run --rm --user "$$(id -u):$$(id -g)" --tmpfs /tmp:rw,exec,size=2g --mount "type=bind,src=$$(pwd)/..,dst=/workspace" -w /workspace/backend -e GOCACHE=/tmp/go-build $(GO_IMAGE) go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) -config ../api/oapi-codegen.yaml ../api/openapi.yaml
	REDOCLY_TELEMETRY=off npx --no-install openapi-typescript openapi.yaml -o generated/api.d.ts
	REDOCLY_TELEMETRY=off npx --no-install openapi-typescript openapi.yaml -o generated/health.d.ts
	docker run --rm --user "$$(id -u):$$(id -g)" --tmpfs /tmp:rw,exec,size=2g --mount "type=bind,src=$$(pwd)/..,dst=/workspace" -w /workspace/backend -e GOCACHE=/tmp/go-build $(GO_IMAGE) go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) -generate types -package httpx -o internal/platform/httpx/health.gen.go ../api/openapi.yaml

validate:
	REDOCLY_TELEMETRY=off npx --no-install redocly lint openapi.yaml --extends=recommended --skip-rule=info-license --skip-rule=info-license-strict
	node --test check-health.test.mjs check-contract.test.mjs
	node check-health.mjs
	node check-generation.mjs

check:
	$(MAKE) validate
	@set -eu; before=$$(mktemp -d); trap 'rm -rf "$$before"' EXIT; cp -a generated "$$before/generated"; cp ../backend/internal/identity/httpapi/setup.gen.go "$$before/setup.gen.go"; cp ../backend/internal/platform/httpx/health.gen.go "$$before/health.gen.go"; $(MAKE) generate; diff -ru "$$before/generated" generated; cmp "$$before/setup.gen.go" ../backend/internal/identity/httpapi/setup.gen.go; cmp "$$before/health.gen.go" ../backend/internal/platform/httpx/health.gen.go
