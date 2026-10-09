OAPI_CODEGEN_VERSION := v2.4.1

.PHONY: generate validate check

generate:
	mkdir -p generated
	cd ../backend && go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) -config ../api/oapi-codegen.yaml ../api/openapi.yaml
	REDOCLY_TELEMETRY=off npx --no-install openapi-typescript openapi.yaml -o generated/api.d.ts
	REDOCLY_TELEMETRY=off npx --no-install openapi-typescript openapi.yaml -o generated/health.d.ts
	cd ../backend && go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) -generate types -package httpx -o internal/platform/httpx/health.gen.go ../api/openapi.yaml
	cd ../backend && go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) -generate types -package httpapi -o internal/household/httpapi/household.gen.go ../api/openapi.yaml
	cd ../backend && go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) -generate types -package httpapi -o internal/subject/httpapi/subject.gen.go ../api/openapi.yaml
	cd ../backend && go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) -generate types -package httpapi -o internal/item/httpapi/item.gen.go ../api/openapi.yaml

validate:
	REDOCLY_TELEMETRY=off npx --no-install redocly lint openapi.yaml --extends=recommended --skip-rule=info-license --skip-rule=info-license-strict
	node --test check-health.test.mjs check-contract.test.mjs oidc-middleware.test.mjs
	node check-health.mjs
	node check-generation.mjs

check:
	$(MAKE) validate
	../scripts/check-api-generation.sh
