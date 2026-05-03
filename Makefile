.PHONY: build clean test lint deps

deps:
	@which ctags > /dev/null 2>&1 && echo "ctags already installed" || \
	  (which brew > /dev/null 2>&1 && brew install universal-ctags || \
	   which apt-get > /dev/null 2>&1 && sudo apt-get install -y universal-ctags || \
	   which dnf > /dev/null 2>&1 && sudo dnf install -y ctags || \
	   echo "ERROR: could not install ctags — install universal-ctags manually")

build:
	go build -buildvcs=false -o bin/menace .
	cp bin/menace menace

clean:
	rm -f bin/menace

test:
	go test ./...

lint:
	golangci-lint run ./...

install: deps build
	cp bin/menace /usr/local/bin/menace
