fmt:
	gofmt -s -w .

test:
	go test ./... -v -test.timeout=30s

race:
	go test ./... -v -race -test.timeout=30s

coverage:
	go test ./... -v -race -test.timeout=30s -coverprofile=coverage.txt -covermode=atomic

.PHONY: coverage fmt race test
