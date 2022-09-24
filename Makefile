# This Makefile is meant to be used by people that do not usually work
# with Go source code. If you know what GOPATH is then you probably
# don't need to bother with make.

.PHONY: geth android ios evm all test truffle-test clean
.PHONY: docker
.PHONY: geth-linux-arm geth-linux-arm64 geth-linux-arm5 geth-linux-arm6 geth-linux-arm7

GOBIN = ./build/bin
GO ?= latest
GORUN = env GO111MODULE=on go run

geth:
	$(GORUN) build/ci.go install ./cmd/geth
	@echo "Done building."
	@echo "Run \"$(GOBIN)/geth\" to launch geth."

geth-linux-arm: geth-linux-arm5 geth-linux-arm6 geth-linux-arm7 geth-linux-arm64

geth-linux-arm5:
	env GO111MODULE=on GOARCH=arm GOARM=5 GOOS=linux go build -o build/bin/geth-linux-arm-5  ./cmd/geth

geth-linux-arm6:
	env GO111MODULE=on GOARCH=arm GOARM=6 GOOS=linux go build -o build/bin/geth-linux-arm-6  ./cmd/geth

geth-linux-arm7:
	env GO111MODULE=on GOARCH=arm GOARM=7 GOOS=linux go build -o build/bin/geth-linux-arm-7  ./cmd/geth

geth-linux-arm64:
	env GO111MODULE=on GOARCH=arm64 GOOS=linux go build -o build/bin/geth-linux-arm64  ./cmd/geth

all:
	$(GORUN) build/ci.go install

android:
	$(GORUN) build/ci.go aar --local
	@echo "Done building."
	@echo "Import \"$(GOBIN)/geth.aar\" to use the library."
	@echo "Import \"$(GOBIN)/geth-sources.jar\" to add javadocs"
	@echo "For more info see https://stackoverflow.com/questions/20994336/android-studio-how-to-attach-javadoc"

ios:
	$(GORUN) build/ci.go xcode --local
	@echo "Done building."
	@echo "Import \"$(GOBIN)/Geth.framework\" to use the library."

test: all
	$(GORUN) build/ci.go test -timeout 1h

truffle-test:
	docker build . -f ./docker/Dockerfile --target bsc-genesis -t bsc-genesis
	docker build . -f ./docker/Dockerfile --target bsc -t bsc
	docker build . -f ./docker/Dockerfile.truffle -t truffle-test
	docker-compose -f ./tests/truffle/docker-compose.yml up genesis
	docker-compose -f ./tests/truffle/docker-compose.yml up -d bsc-rpc bsc-validator1
	sleep 30
	docker-compose -f ./tests/truffle/docker-compose.yml up --exit-code-from truffle-test truffle-test
	docker-compose -f ./tests/truffle/docker-compose.yml down

lint: ## Run linters.
	$(GORUN) build/ci.go lint

clean:
	env GO111MODULE=on go clean -cache
	rm -fr build/_workspace/pkg/ $(GOBIN)/*

# The devtools target installs tools required for 'go generate'.
# You need to put $GOBIN (or $GOPATH/bin) in your PATH to use 'go generate'.

devtools:
	env GOBIN= go install golang.org/x/tools/cmd/stringer@latest
	env GOBIN= go install github.com/kevinburke/go-bindata/go-bindata@latest
	env GOBIN= go install github.com/fjl/gencodec@latest
	env GOBIN= go install github.com/golang/protobuf/protoc-gen-go@latest
	env GOBIN= go install ./cmd/abigen
	@type "solc" 2> /dev/null || echo 'Please install solc'
	@type "protoc" 2> /dev/null || echo 'Please install protoc'

docker:
	docker build --pull -t bnb-chain/bsc:latest -f Dockerfile .
