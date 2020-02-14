fmt:
	gofmt -s -w .

test:
	go test ./... -v -test.timeout=30s

race:
	go test ./... -v -race -test.timeout=30s

.PHONY: fmt race test
