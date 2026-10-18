# Local secrets. Make reads KEY=value lines as make variables, so do not put quotes around values.
-include .env
export YOUTUBE_API_KEY

LEAGUE ?= worlds
FILTER ?= Worlds 2026
PORT   ?= 8000

.PHONY: help run serve test lint update

help: ## Show targets
	@grep -hE '^[a-z]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-8s %s\n", $$1, $$2}'
	@echo "  Example: make run LEAGUE=demacia_cup FILTER='Demacia Cup'"

run: ## Fetch schedule and uploads, write docs/data/<tournament>.json
	@[ -n "$$YOUTUBE_API_KEY" ] || echo "Warning: YOUTUBE_API_KEY is not set, using RSS (last ~15 uploads only)"
	go run ./cmd/vodsync -league '$(LEAGUE)' -title-filter '$(FILTER)'

serve: ## Serve docs/ locally (PORT=8000)
	python3 -m http.server -d docs $(PORT)

test: ## Run unit tests
	go test ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

update: run ## Run, then commit and push docs/data
	git add docs/data
	git diff --cached --quiet -- docs/data || git commit -m "docs: Update data" -- docs/data
	git push
