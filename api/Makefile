OAPI_CODEGEN_VERSION := v2.8.2

.PHONY: generate validate check

generate:
	mkdir -p generated
	cd ../backend && go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION) -config ../api/oapi-codegen.yaml ../api/openapi.yaml
	REDOCLY_TELEMETRY=off npx --no-install openapi-typescript openapi.yaml -o generated/api.d.ts

validate:
	REDOCLY_TELEMETRY=off npx --no-install redocly lint openapi.yaml --extends=recommended --skip-rule=info-license --skip-rule=info-license-strict
	npm test
	node check-health.mjs
	node check-generation.mjs

check:
	$(MAKE) validate
	../scripts/check-api-generation.sh
