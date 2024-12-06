COMMIT := $(shell jj log --template 'commit_id.short(8)' --no-graph --limit 1)
VERSION := development build ($(COMMIT))
LDFLAGS := -X 'main.version=$(VERSION)'

.PHONY: build linux

build:
	go build -ldflags "$(LDFLAGS)"

linux:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)"
