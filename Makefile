.PHONY: clean lint build.local build.linux build.osx build.docker build.push

BINARY               ?= clm
VERSION              ?= $(shell git describe --tags --always --dirty)
IMAGE                ?= registry-write.opensource.zalan.do/teapot/cluster-lifecycle-manager
TAG                  ?= $(VERSION)
SOURCES              = $(shell find . -name '*.go')
GO                   ?= go
SPEC                 = docs/cluster-registry.yaml
CR_CLIENT            = pkg/cluster-registry
DOCKERFILE           ?= Dockerfile
GOPKGS               = $(shell $(GO) list ./...)
GO_SWAGGER           = ./build/swagger
BUILD_FLAGS          ?= -v
LDFLAGS              ?= -X main.version=$(VERSION) -w -s

default: build.local

clean:
	rm -rf build
	rm -rf $(CR_CLIENT)
	rm -rf $(AWS_INSTANCE_DATA)

test: $(AWS_INSTANCE_DATA)
	$(GO) test -v -race -coverprofile=profile.cov $(GOPKGS)
	$(GO) vet -v $(GOPKGS)

lint: $(SOURCES) $(AWS_INSTANCE_DATA)
	$(GO) mod download
	golangci-lint -v run ./...

fmt:
	$(GO) fmt $(GOPKGS)

$(AWS_DATA_SRC):
	mkdir -p $(dir $@)
	curl -L -s --fail https://www.ec2instances.info/instances.json | jq '[.[] | {instance_type, vCPU, memory, storage: (if .storage == null then null else .storage | {devices, size, nvme_ssd} end)}] | sort_by(.instance_type)' > "$@"

build.local: build/$(BINARY)
build.linux: build/linux/$(BINARY)
build.osx: build/osx/$(BINARY)
build.linux.amd64: build/linux/amd64/$(BINARY)
build.linux.arm64: build/linux/arm64/$(BINARY)

build/$(BINARY): $(SOURCES) $(AWS_INSTANCE_DATA)
	CGO_ENABLED=0 $(GO) build -o build/$(BINARY) $(BUILD_FLAGS) -ldflags "$(LDFLAGS)" ./cmd/$(BINARY)

build/linux/$(BINARY): $(SOURCES) $(AWS_INSTANCE_DATA)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build $(BUILD_FLAGS) -o build/linux/$(BINARY) -ldflags "$(LDFLAGS)" ./cmd/$(BINARY)

build/linux/amd64/%: go.mod $(SOURCES)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build $(BUILD_FLAGS) -o build/linux/amd64/$(notdir $@) -ldflags "$(LDFLAGS)" ./cmd/$(notdir $@)

build/linux/arm64/%: go.mod $(SOURCES)
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build $(BUILD_FLAGS) -o build/linux/arm64/$(notdir $@) -ldflags "$(LDFLAGS)" ./cmd/$(notdir $@)

build/osx/$(BINARY): $(SOURCES) $(AWS_INSTANCE_DATA)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GO) build $(BUILD_FLAGS) -o build/osx/$(BINARY) -ldflags "$(LDFLAGS)" ./cmd/$(BINARY)

build.docker: build.linux
	docker build --rm -t "$(IMAGE):$(TAG)" -f $(DOCKERFILE) --build-arg TARGETARCH= .

build.push: build.docker
	docker push "$(IMAGE):$(TAG)"
