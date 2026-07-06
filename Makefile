.PHONY: build test web clean release

build:
	CGO_ENABLED=0 go build -o bin/fleet ./cmd/fleet

release: # usage: make release VERSION=v0.1.0
	scripts/release.sh $(VERSION)

test:
	go test ./...

web:
	cd web && npm run build

clean:
	rm -rf bin
