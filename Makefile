.PHONY: test check run
test:
	go test ./...
check:
	go vet ./...
run:
	go run ./cmd/gateway
