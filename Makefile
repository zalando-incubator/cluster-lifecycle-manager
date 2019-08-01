.PHONY: clean check build.local build.linux build.osx build.docker build.push

BINARY               ?= clm
VERSION              ?= $(shell git describe --tags --always --dirty)
IMAGE                ?= registry-write.opensource.zalan.do/teapot/cluster-lifecycle-manager
TAG                  ?= $(VERSION)
SOURCES              = $(shell find . -name '*.go')
GO                   ?= go
SPEC                 = docs/cluster-registry.yaml
CR_CLIENT            = pkg/cluster-registry
AWS_INSTANCE_DATA    = pkg/aws/bindata.go
AWS_DATA_SRC         = awsdata/instances.json
DOCKERFILE           ?= Dockerfile
GOPKGS               = $(shell $(GO) list ./...)
GO_SWAGGER           = ./build/swagger
GO_SWAGGER_VERSION   = 4c3aa23a8c9ac15f4fd63c3e28f12bd20e387f62 # should match the version in go.mod
GO_BINDATA           = ./build/go-bindata
GO_BINDATA_VERSION   = 6025e8de665b31fa74ab1a66f2cddd8c0abf887e # should match the version in go.mod
BUILD_FLAGS          ?= -v
LDFLAGS              ?= -X main.version=$(VERSION) -w -s

default: build.local

clean:
	rm -rf build
	rm -rf $(CR_CLIENT)
	rm -rf $(AWS_INSTANCE_DATA)

test: $(CR_CLIENT) $(AWS_INSTANCE_DATA)
	$(GO) test -v -race $(GOPKGS)
	$(GO) vet -v $(GOPKGS)

lint:
	go mod download && make && golangci-lint run ./...

fmt:
	$(GO) fmt $(GOPKGS)

$(AWS_DATA_SRC):
	mkdir -p $(dir $@)
	curl -L -s --fail https://www.ec2instances.info/instances.json | jq '[.[] | {instance_type, vCPU, memory, ebs_as_nvme, storage: (if .storage == null then null else .storage | {devices, nvme_ssd, storage_needs_initialization} end)}] | sort_by(.instance_type)' > "$@"

$(GO_BINDATA):
	mkdir -p build
	GOBIN=$(shell pwd)/build $(GO) get github.com/jteeuwen/go-bindata/go-bindata@$(GO_BINDATA_VERSION)

$(AWS_INSTANCE_DATA): $(GO_BINDATA) $(AWS_DATA_SRC)
	$(GO_BINDATA) -pkg aws -o $(AWS_INSTANCE_DATA) --prefix $(dir $(AWS_DATA_SRC)) $(AWS_DATA_SRC)
	echo '//lint:file-ignore ST1005 Ignore issues with generated code' >> $(AWS_INSTANCE_DATA)

$(GO_SWAGGER):
	mkdir -p build
	GOBIN=$(shell pwd)/build $(GO) get github.com/go-swagger/go-swagger/cmd/swagger@$(GO_SWAGGER_VERSION)

$(CR_CLIENT): $(GO_SWAGGER) $(SPEC)
	mkdir -p $@
	touch $@
	$(GO_SWAGGER) generate client --name cluster-registry --principal oauth.User --spec docs/cluster-registry.yaml --target ./$(CR_CLIENT)

build.local: build/$(BINARY)
build.linux: build/linux/$(BINARY)
build.osx: build/osx/$(BINARY)

build/$(BINARY): $(CR_CLIENT) $(SOURCES) $(AWS_INSTANCE_DATA)
	CGO_ENABLED=0 $(GO) build -o build/$(BINARY) $(BUILD_FLAGS) -ldflags "$(LDFLAGS)" ./cmd/$(BINARY)

build/linux/$(BINARY): $(CR_CLIENT) $(SOURCES) $(AWS_INSTANCE_DATA)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build $(BUILD_FLAGS) -o build/linux/$(BINARY) -ldflags "$(LDFLAGS)" ./cmd/$(BINARY)

build/osx/$(BINARY): $(CR_CLIENT) $(SOURCES) $(AWS_INSTANCE_DATA)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GO) build $(BUILD_FLAGS) -o build/osx/$(BINARY) -ldflags "$(LDFLAGS)" ./cmd/$(BINARY)

build.docker: build.linux
	docker build --rm -t "$(IMAGE):$(TAG)" -f $(DOCKERFILE) .

build.push: build.docker
	docker push "$(IMAGE):$(TAG)"
