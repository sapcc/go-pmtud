IMAGE	 := keppel.eu-de-1.cloud.sap/ccloud/go-pmtud
DATE     := $(shell date +%Y%m%d%H%M%S)
VERSION  ?= v$(DATE)

.PHONY: all

all: build push

build:
	docker build -t $(IMAGE):$(VERSION) .

push: build
	docker push ${IMAGE}:${VERSION}

docker-push-mac:
	docker buildx build  --platform linux/amd64 . -t ${IMAGE}:${VERSION} --push

## For https://github.com/sapcc/go-makefile-maker

# which packages to test with "go test"
GO_TESTPKGS := $(shell go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...)
# which packages to measure coverage for
GO_COVERPKGS := $(shell go list ./...)

.PHONY: license-headers
license-headers: FORCE
	@if ! hash addlicense 2>/dev/null; then printf "\e[1;36m>> Installing addlicense...\e[0m\n"; go install github.com/google/addlicense@latest; fi
	find * \( -name vendor -type d -prune \) -o \( -name \*.go -exec addlicense -c "SAP SE" -- {} + \)

check-dependency-licenses: FORCE prepare-static-check
	@printf "\e[1;36m>> go-licence-detector\e[0m\n"
	@go list -m -mod=readonly -json all | go-licence-detector -includeIndirect -rules .license-scan-rules.json -overrides .license-scan-overrides.jsonl

prepare-static-check: FORCE
	@if ! hash golangci-lint 2>/dev/null; then printf "\e[1;36m>> Installing golangci-lint (this may take a while)...\e[0m\n"; go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest; fi
	@if ! hash go-licence-detector 2>/dev/null; then printf "\e[1;36m>> Installing go-licence-detector...\e[0m\n"; go install go.elastic.co/go-licence-detector@latest; fi
	@if ! hash addlicense 2>/dev/null; then  printf "\e[1;36m>> Installing addlicense...\e[0m\n";  go install github.com/google/addlicense@latest; fi

check-license-headers: FORCE prepare-static-check
	@printf "\e[1;36m>> addlicense --check\e[0m\n"
	@addlicense --check  -- $(patsubst $(shell awk '$$1 == "module" {print $$2}' go.mod)%,.%/*.go,$(shell go list ./...))

bin/cover.out: FORCE | build
	@printf "\e[1;36m>> go test\e[0m\n"
	@env $(GO_TESTENV) go test $(GO_BUILDFLAGS) -ldflags '-s -w $(GO_LDFLAGS)' -shuffle=on -p 1 -coverprofile=$@ -covermode=count -coverpkg=$(subst $(space),$(comma),$(GO_COVERPKGS)) $(GO_TESTPKGS)

bin/cover.html: bin/cover.out
	@printf "\e[1;36m>> go tool cover > bin/cover.html\e[0m\n"
	@go tool cover -html $< -o $@

vars: FORCE
	@printf "DESTDIR=$(DESTDIR)\n"
	@printf "GO_BUILDFLAGS=$(GO_BUILDFLAGS)\n"
	@printf "GO_COVERPKGS=$(GO_COVERPKGS)\n"
	@printf "GO_LDFLAGS=$(GO_LDFLAGS)\n"
	@printf "GO_TESTENV=$(GO_TESTENV)\n"
	@printf "GO_TESTPKGS=$(GO_TESTPKGS)\n"
	@printf "PREFIX=$(PREFIX)\n"

.PHONY: FORCE
