APP_NAME := fervid-budget
GO ?= go

.PHONY: help fmt vet test test-race test-cover typecheck test-e2e test-audit test-all run seed backup manual

help:
	@echo "$(APP_NAME) targets:"
	@echo "  make fmt   - format Go sources"
	@echo "  make vet   - run Go static analysis"
	@echo "  make test  - run Go tests"
	@echo "  make test-race - run Go tests with the race detector"
	@echo "  make test-cover - run Go tests with coverage"
	@echo "  make typecheck - strictly type-check Playwright tests"
	@echo "  make test-e2e - run Playwright browser tests"
	@echo "  make test-audit - run the QA audit suite, one server per area"
	@echo "  make test-all - run Go and Playwright tests"
	@echo "  make manual - re-photograph every screen and rebuild docs/manual"
	@echo "  make run   - start the local server"
	@echo "  make seed  - create admin/sample data if empty"
	@echo "  make backup - create a local backup"

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

test-cover:
	$(GO) test ./... -coverprofile=output/coverage.out

typecheck:
	npm run typecheck

test-e2e:
	npm run test:e2e

# The audit suite is not part of test-e2e. It builds a world to interrogate —
# custom roles, dozens of users, 214 requests — so it needs a database per area
# rather than the one shared server test-e2e uses. See docs/qa/README.md.
test-audit:
	scripts/run-audit.sh

test-all: vet test-race test-cover typecheck test-e2e

# Re-photographs every screen and rebuilds the manual from the results.
#
# Unlike test-e2e, this needs YOUR server already running on :8080 against
# data/fervid.db — the throwaway server the Playwright config starts is freshly
# seeded with no requests, and a manual illustrated with empty queues would be
# worthless. Start one with `make run` first.
#
# The build fails if a page names a screenshot that does not exist, if a
# screenshot is never shown, or if an internal link points nowhere.
manual:
	npx playwright test --config tests/manual/playwright.manual.config.ts
	node tests/manual/build/build.mts

run:
	$(GO) run ./cmd/server

seed:
	$(GO) run ./cmd/server --seed

backup:
	$(GO) run ./cmd/server --backup
