.PHONY: build test web clean

build:
	CGO_ENABLED=0 go build -o bin/fleet ./cmd/fleet

test:
	go test ./...

web:
	cd web && npm run build

clean:
	rm -rf bin
