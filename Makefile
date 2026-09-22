.PHONY: build build-tui build-daemon test check
build: build-tui build-daemon

build-tui:
	go build -buildvcs=false -o tidesms ./cmd/tidesms

build-daemon:
	go build -buildvcs=false -o tidesms-daemon ./cmd/tidesms-daemon

test:
	go test -race ./...

check:
	go build -buildvcs=false ./...
	go vet ./...
	go test -race ./...
	golangci-lint run
