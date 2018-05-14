.PHONY: clean check build.local build.linux build.osx build.docker build.push

BINARY               ?= clm
VERSION              ?= $(shell git describe --tags --always --dirty)
IMAGE                ?= registry-write.opensource.zalan.do/teapot/cluster-lifecycle-manager
TAG                  ?= $(VERSION)
SOURCES              = $(shell find . -name '*.go')
SPEC                 = docs/cluster-registry.yaml
CR_CLIENT            = pkg/cluster-registry
AWS_INSTANCE_DATA    = pkg/aws/bindata.go
AWS_DATA_SRC         = awsdata/instances.json
DOCKERFILE           ?= Dockerfile
GOPKGS               = $(shell go list ./...)
GO_SWAGGER           = ./build/swagger
GO_SWAGGER_SRC       = ./vendor/github.com/go-swagger/go-swagger/cmd/swagger/
GO_BINDATA           = ./build/go-bindata
GO_BINDATA_SRC       = ./vendor/github.com/jteeuwen/go-bindata/go-bindata/
BUILD_FLAGS          ?= -v
LDFLAGS              ?= -X main.version=$(VERSION) -w -s

default: build.local

clean:
	rm -rf build
	rm -rf $(CR_CLIENT)
	rm -rf $(AWS_INSTANCE_DATA)

test: $(CR_CLIENT) $(AWS_INSTANCE_DATA)
	go test -v -race $(GOPKGS)

fmt:
	go fmt $(GOPKGS)

check:
	golint $(GOPKGS)
	go vet -v $(GOPKGS)

$(AWS_DATA_SRC):
	mkdir -p $(dir $@)
	curl -s --fail https://www.ec2instances.info/instances.json -o $@

$(GO_BINDATA):
	mkdir -p build
	go build -o $@ $(GO_BINDATA_SRC)

$(AWS_INSTANCE_DATA): $(GO_BINDATA) $(AWS_DATA_SRC)
	$(GO_BINDATA) -pkg aws -o $(AWS_INSTANCE_DATA) --prefix $(dir $(AWS_DATA_SRC)) $(AWS_DATA_SRC)

$(GO_SWAGGER):
	mkdir -p build
	go build -o $@ $(GO_SWAGGER_SRC)

$(CR_CLIENT): $(GO_SWAGGER) $(SPEC)
	mkdir -p $@
	touch $@
	$(GO_SWAGGER) generate client --name cluster-registry --principal oauth.User --spec docs/cluster-registry.yaml --target ./$(CR_CLIENT)

build.local: build/$(BINARY)
build.linux: build/linux/$(BINARY)
build.osx: build/osx/$(BINARY)

build/$(BINARY): $(CR_CLIENT) $(SOURCES) $(AWS_INSTANCE_DATA)
	CGO_ENABLED=0 go build -o build/$(BINARY) $(BUILD_FLAGS) -ldflags "$(LDFLAGS)" ./cmd/$(BINARY)

build/linux/$(BINARY): $(CR_CLIENT) $(SOURCES) $(AWS_INSTANCE_DATA)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(BUILD_FLAGS) -o build/linux/$(BINARY) -ldflags "$(LDFLAGS)" ./cmd/$(BINARY)

build/osx/$(BINARY): $(CR_CLIENT) $(SOURCES) $(AWS_INSTANCE_DATA)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build $(BUILD_FLAGS) -o build/osx/$(BINARY) -ldflags "$(LDFLAGS)" ./cmd/$(BINARY)

build.docker: build.linux
	docker build --rm -t "$(IMAGE):$(TAG)" -f $(DOCKERFILE) .

build.push: build.docker
	docker push "$(IMAGE):$(TAG)"
