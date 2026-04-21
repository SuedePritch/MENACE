.PHONY: build clean test lint

build:
	go build -buildvcs=false -o bin/menace .
	cp bin/menace menace

clean:
	rm -f bin/menace

test:
	go test ./...

lint:
	golangci-lint run ./...

install: build
	cp bin/menace /usr/local/bin/menace
