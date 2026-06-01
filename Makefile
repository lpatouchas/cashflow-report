BINARY := cashflow-report

# Defaults match the README; override on the command line, e.g.
#   make generate DATA=./exports OUT=./out.html
#   make serve ADDR=:1234
DATA ?= ./data
OUT  ?= ./report.html
ADDR ?= :8080

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the binary
	go build -o $(BINARY) .

.PHONY: serve
serve: build ## Build and start the web app (ADDR=:8080)
	./$(BINARY) serve --addr $(ADDR)

.PHONY: generate
generate: build ## Build and generate a report headlessly (DATA=./data OUT=./report.html)
	./$(BINARY) generate --data $(DATA) --out $(OUT)

.PHONY: test
test: ## Run the test suite
	go test ./...

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY) coverage.out
