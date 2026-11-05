.PHONY: check run demo build
check:
	go test -race ./...
	go vet ./...
run:
	go run ./cmd/palstudio
demo:
	go run ./cmd/paltool demo
build:
	mkdir -p bin
	go build -o bin/palstudio ./cmd/palstudio
	go build -o bin/paltool ./cmd/paltool
	go build -o bin/palverify ./cmd/palverify
