fmt:
	gofmt -s -w .

test:
	go test ./... -v -test.timeout=5s

race:
	go test ./... -v -race -test.timeout=5s

coverage:
	go test ./... -v -race -test.timeout=5s -coverprofile=coverage.txt -covermode=atomic
	go tool cover -html=coverage.txt -o coverage.html

.PHONY: coverage fmt race test
