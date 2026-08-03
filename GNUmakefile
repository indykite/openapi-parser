.PHONY: default fmt goimports gci lint lint-fix test tidy install-tools parity

GO111MODULE=on

# $(PWD) doesn't work on Windows, use this
mkfile_path := $(abspath $(lastword $(MAKEFILE_LIST)))
current_dir := $(patsubst %/,%,$(dir $(mkfile_path)))

default:

fmt:
	@echo "==> Fixing source code with gofmt..."
	gofmt -s -w $$(go list -f "{{.Dir}}" ./...)

# goimports was replaced with GCI, but for backward compatibility what most of us remember, keep it here as well
goimports: gci

gci:
	@echo "==> Fixing imports order with gci..."
	gci write -s standard -s default -s "prefix(github.com/indykite/openapi-parser)" -s blank -s dot .

lint:
	@echo "==> Checking source code against linters..."
	@golangci-lint run ./...

lint-fix:
	@echo "==> Checking source code against linters (with fixes)..."
	@golangci-lint run --fix ./...

test:
	# Do not add -p flag. When tests are parallelized, it is actually more time consuming. Locally and in CI too
	go test -v ./...

tidy:
	go mod tidy

# Compare oasgen output against swag-generated docs in another repository.
# A "service" is any directory with its own docs/swagger.yaml baseline; they
# are auto-discovered, and a single-binary repo has one at its root. Pass
# SERVICES to restrict the list. Example: make parity REPO=../my-platform
parity:
	@test -n "$(REPO)" || { echo "usage: make parity REPO=path/to/repo [SERVICES=dir1,dir2]"; exit 2; }
	@echo "==> Checking op/schema parity against swag output in $(REPO)..."
	@go run ./cmd/oasparity -repo "$(REPO)" $(if $(SERVICES),-services "$(SERVICES)")

install-tools:
	@echo Installing tools
	@go install github.com/daixiang0/gci@latest
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	@echo Installation completed
