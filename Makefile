APP_NAME := fervid-budget
GO ?= go

.PHONY: help fmt test run seed backup

help:
	@echo "$(APP_NAME) targets:"
	@echo "  make fmt   - format Go sources"
	@echo "  make test  - run Go tests"
	@echo "  make run   - start the local server"
	@echo "  make seed  - create admin/sample data if empty"
	@echo "  make backup - create a local backup"

fmt:
	$(GO) fmt ./...

test:
	$(GO) test ./...

run:
	$(GO) run ./cmd/server

seed:
	$(GO) run ./cmd/server --seed

backup:
	$(GO) run ./cmd/server --backup
