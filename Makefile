BIN := mdv
PREFIX ?= $(HOME)/.local

.PHONY: all tidy test build install clean

all: tidy test build

tidy:
	go mod tidy

test:
	go vet ./...
	go test ./...

build:
	go build -o $(BIN) .

install: all
	install -d $(PREFIX)/bin
	install -m 755 $(BIN) $(PREFIX)/bin/$(BIN)

clean:
	rm -f $(BIN)
