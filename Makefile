.PHONY: admin-build admin-dev build check check-dev check-fast check-full cli-clean-install-test demo dependency-check documentation-check dogfood-new format format-check go-format-check go-test go-test-after-vet go-test-dev go-vet go-vuln-check install lint mongodb-fixture-test mongodb-generated-project-test mongodb-production-test mongodb-test packed-release-check performance-check playground-ridu-dev playground-ridu-down playground-ridu-hydrate playground-ridu-reset postgres-browser-test postgres-test release-build-check release-check release-version-check runtime-packages-build security-check sqlite-no-cgo-test sqlite-payload-baseline-test sqlite-race-test sqlite-smoke-test sqlite-test test typecheck

build:
	bun run build

admin-build:
	cd admin && bun run build

admin-dev:
	go run ./internal/dogfood/admin

demo: admin-build
	RIDU_BROWSER_ADDRESS=127.0.0.1:8080 go run ./tests/contracts/admin_server

dogfood-new:
	@test -n "$(TARGET)" -o -n "$(filter 1 true yes,$(INTERACTIVE))" || (echo 'usage: make dogfood-new TARGET=/path/to/new-project [INTERACTIVE=1]' && exit 2)
	go run ./internal/dogfood/new $(if $(strip $(TARGET)),--target "$(TARGET)",) $(if $(filter 1 true yes,$(INTERACTIVE)),--interactive,)

playground-ridu-hydrate:
	go run ./internal/dogfood/playground --target ./playground/ridu

playground-ridu-dev: playground-ridu-hydrate
	docker compose -p ridu-playground -f ./playground/ridu/compose.yaml up -d postgres
	cd playground/ridu && ./.ridu/bin/ridu dev --no-docker

playground-ridu-down:
	docker compose -p ridu-playground -f ./playground/ridu/compose.yaml down

playground-ridu-reset:
	docker compose -p ridu-playground -f ./playground/ridu/compose.yaml down --volumes

check: check-fast

check-dev: go-format-check go-test-dev
	bun run check:dev

check-fast: go-format-check go-test-after-vet
	bun run check:fast

# Browser fixtures consume completed static builds. Go tests can overlap them,
# but not frontend checks: framework-package tests rebuild the runtime packages.
check-full: go-format-check
	bun run check:full:repository

dependency-check:
	go mod verify
	bun audit --audit-level=high
	cd website && bun audit --audit-level=high

documentation-check:
	bun run check:website:clean
	go test ./examples/documentation/...
	bun run prepare:website
	cd website && bun run verify

go-vuln-check:
	go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

security-check: dependency-check go-vuln-check

release-build-check:
	bash ./scripts/check-release-reproducibility.sh
	bash ./scripts/check-release-artifacts-reproducibility.sh

release-version-check:
	bun ./scripts/check-release-version.ts

packed-release-check: release-version-check
	bash ./scripts/check-packed-release.sh

release-check:
	$(MAKE) check-full RIDU_TEST_CLEAN_CACHE=true GOFLAGS="$(GOFLAGS) -count=1" RIDU_POSTGRES_URL= RIDU_POSTGRES_RECOVERY_DRILL= RIDU_POSTGRES_CLIENT_IMAGE=
	$(MAKE) performance-check RIDU_POSTGRES_URL= RIDU_POSTGRES_RECOVERY_DRILL= RIDU_POSTGRES_CLIENT_IMAGE=
	$(MAKE) sqlite-test RIDU_POSTGRES_URL= RIDU_POSTGRES_RECOVERY_DRILL= RIDU_POSTGRES_CLIENT_IMAGE=
	$(MAKE) postgres-test
	$(MAKE) postgres-browser-test
	$(MAKE) mongodb-fixture-test
	$(MAKE) documentation-check
	$(MAKE) security-check
	$(MAKE) release-build-check
	$(MAKE) packed-release-check

format:
	find . -type d \( -name .git -o -name .ridu -o -name node_modules -o -path ./playground \) -prune -o -type f -name '*.go' -exec gofmt -w {} +
	bun run format

format-check: go-format-check
	bun run format:check

go-format-check:
	@unformatted="$$(find . -type d \( -name .git -o -name .ridu -o -name node_modules -o -path ./playground \) -prune -o -type f -name '*.go' -exec gofmt -l {} +)"; \
	if [ -n "$$unformatted" ]; then printf 'Go files need formatting:\n%s\n' "$$unformatted"; exit 1; fi

# Go example and generated-consumer tests require the built TypeScript packages.
# Prepare them before every Go test entry point, including short tests.
runtime-packages-build:
	bun run build:runtime-packages

go-test: runtime-packages-build
	go test ./...

go-test-after-vet: go-vet runtime-packages-build
	go test -vet=off ./...

go-test-dev: go-vet runtime-packages-build
	go test -short -vet=off ./...

go-vet:
	go vet ./...

# Fresh dependency/build caches qualify the real install path independently of
# the content-addressed development cache. release-check uses this mode for its
# complete check-full invocation.
cli-clean-install-test:
	RIDU_TEST_CLEAN_CACHE=true go test -count=1 ./internal/cli -run '^Test(NewReleaseOverrideWorksThroughRealBinary|GeneratedProjectCompilesAndGeneratesOutsideRepository)$$'

# Keep scale measurements serial and outside correctness-gate CPU contention.
performance-check:
	bun run build:runtime-packages
	go test -run '^$$' -bench '^BenchmarkEmbeddedHookBatch$$' -benchmem -benchtime=1x ./core
	go test -run '^$$' -bench '^(BenchmarkEmbeddedValueScaling|BenchmarkNativeValueScaling)$$/^None$$' -benchmem -benchtime=1x ./core
	go test -run '^$$' -bench '^(BenchmarkEmbeddedValueScaling|BenchmarkNativeValueScaling)$$/^ReadAll$$/^100$$' -benchmem -benchtime=1x ./core
	go test -run '^$$' -bench '^BenchmarkOrdinaryValueScaling$$/^(native|embedded)$$/./^100$$' -benchmem -benchtime=1x ./core
	go test -run '^$$' -bench '^BenchmarkLocaleViewControls$$/^(native|embedded)$$/./^100$$' -benchmem -benchtime=1x ./core
	go test -run '^$$' -bench '^BenchmarkOrdinaryLocaleSerialization$$/^(native|embedded)$$/^100$$' -benchmem -benchtime=1x ./core
	go test -run '^$$' -bench '^BenchmarkPopulationTraversal$$' -benchmem -benchtime=1x ./internal/population
	RIDU_TEST_GENERATION_PERF=true go test -count=1 ./internal/generate -run '^(TestReusableBlocksGeneratedGrowth|TestRichTextRepeatedDefinitionGrowth)$$'
	bun run build:admin-fixture
	RIDU_ADMIN_FIXTURE_PREBUILT=true bun run test:performance

sqlite-test: sqlite-race-test sqlite-no-cgo-test sqlite-smoke-test sqlite-payload-baseline-test

sqlite-race-test:
	go test -race -count=1 ./adapters/sqlite

sqlite-no-cgo-test:
	CGO_ENABLED=0 go test -count=1 ./adapters/sqlite

sqlite-smoke-test:
	bun run build:runtime-packages
	bun run build:admin-fixture
	RIDU_POSTGRES_URL= RIDU_POSTGRES_RECOVERY_DRILL= RIDU_POSTGRES_CLIENT_IMAGE= bun run test:live-sdk:sqlite
	RIDU_BROWSER_BOOTSTRAP= RIDU_BROWSER_ADDRESS= RIDU_BROWSER_PREVIEW_ADDRESS= RIDU_BROWSER_RESET_TOKEN=ridu-playwright-sqlite-v1 RIDU_SQLITE_FIXTURE= \
		RIDU_POSTGRES_URL= RIDU_POSTGRES_RECOVERY_DRILL= RIDU_POSTGRES_CLIENT_IMAGE= RIDU_ADMIN_FIXTURE_PREBUILT=true bun run test:e2e:sqlite

sqlite-payload-baseline-test:
	bun install --cwd tests/contracts/payload_server --frozen-lockfile
	bun run --cwd tests/contracts/payload_server check:sqlite-side-by-side:payload
	go test -count=1 ./adapters/sqlite -run '^TestSQLitePayloadBaselineWorkflow$$'

postgres-test:
	@test -n "$(RIDU_POSTGRES_URL)" || (echo 'RIDU_POSTGRES_URL is required for mandatory PostgreSQL tests' && exit 2)
	go test -race -count=1 ./adapters/postgres
	go test -count=1 ./internal/cli -run '^TestGeneratedProjectMigratesAgainstPostgres$$'
	RIDU_GRAPHQL_POSTGRES_URL="$(RIDU_POSTGRES_URL)" go test -race -count=1 ./plugins/graphql -run '^TestGraphQLPostgresCRUDLocalizationAndPopulation$$'

postgres-browser-test:
	@test -n "$(RIDU_POSTGRES_URL)" || (echo 'RIDU_POSTGRES_URL is required for PostgreSQL browser tests' && exit 2)
	bun run build:admin-fixture
	RIDU_ADMIN_FIXTURE_PREBUILT=true bun run test:e2e
	RIDU_ADMIN_FIXTURE_PREBUILT=true bun run test:e2e:bootstrap

mongodb-test:
	@test -n "$$RIDU_MONGODB_URL" || (echo 'RIDU_MONGODB_URL is required for mandatory MongoDB adapter tests' && exit 2)
	@go test -race -count=1 ./adapters/mongodb

mongodb-generated-project-test:
	RIDU_MONGODB_GENERATED_PROJECT_TEST=true go test -count=1 -timeout=15m ./internal/cli -run '^TestGeneratedMongoDBDevelopmentProject$$'

mongodb-production-test:
	RIDU_MONGODB_PRODUCTION_FIXTURE_TEST=true go test -race -count=1 -timeout=20m ./adapters/mongodb -run '^(TestMongoDBProductionReplicaSetTLSAuthenticationAndElection|TestMongoDBV2SemanticMigrationLifecycleOnAuthenticatedReplicaSet)$$'
	RIDU_MONGODB_PRODUCTION_DEPLOYMENT_TEST=true go test -race -count=1 -timeout=60m ./adapters/mongodb -run '^TestGeneratedMongoDBProductionDeployment$$'

mongodb-fixture-test:
	@set -e; \
		project="ridu-mongodb-adapter-$$(date +%s)-$$$$"; \
		cleanup() { docker compose -p "$$project" -f ./adapters/mongodb/testdata/compose.yaml down --volumes --remove-orphans; }; \
		trap cleanup EXIT INT TERM; \
		docker compose -p "$$project" -f ./adapters/mongodb/testdata/compose.yaml up -d --wait mongodb mongodb-standalone; \
		address=$$(docker compose -p "$$project" -f ./adapters/mongodb/testdata/compose.yaml port mongodb 27017); \
		standalone_address=$$(docker compose -p "$$project" -f ./adapters/mongodb/testdata/compose.yaml port mongodb-standalone 27017); \
		RIDU_MONGODB_URL="mongodb://$$address/ridu?directConnection=true&replicaSet=ridu-rs0" \
		RIDU_MONGODB_STANDALONE_URL="mongodb://$$standalone_address/ridu?directConnection=true" \
		$(MAKE) mongodb-test
	$(MAKE) mongodb-generated-project-test
	$(MAKE) mongodb-production-test

install:
	bun install --frozen-lockfile

lint:
	bun run lint

test: go-test
	bun run test

typecheck:
	bun run check:types
