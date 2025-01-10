SHELL := /bin/bash
APP_NAME := $(shell basename $(shell pwd))
# APP_NAME := $(shell git remote get-url origin | awk '{split($$0,a,"/");print a[2]}' | sed 's/\.git//g')

.PHONY: help # Show this help
help: .gitignore bin/
	@printf "Available targets for \033[92m$(APP_NAME)\033[0m:\n\n"
	@cat Makefile  | grep ".PHONY" | grep -v ".PHONY: _" | awk '{split($$0,a,".PHONY: ");split(a[2],b,"#");print "\033[36m"b[1]"\033[0m#"b[2]}' | column -s '#' -t


.PHONY: build # Build the binary
build: bin/$(APP_NAME)
bin/$(APP_NAME): \
go.mod \
block/ \
$(find block | grep -E '\.go$$') \
config/ \
$(find config | grep -E '\.go$$') \
encoding/ \
$(find encoding | grep -E '\.go$$') \
error_handling/ \
$(find error_handling | grep -E '\.go$$') \
rpc/ \
$(find rpc | grep -E '\.go$$') \
seq \
$(find seq | grep -E '\.go$$') \
main.go
	go build -o bin/$(APP_NAME) .


.PHONY: run # Run the binary
run: build
	./bin/$(APP_NAME) --help


.gitignore:
	printf "/bin/\n" > .gitignore


bin:
	mkdir -p bin


go.mod:
	go mod init $(APP_NAME)


main.go:
	printf "package main--import (-	'fmt'-)--func main() {-	fmt.Println('Hello, World!')-}-" \
		| tr '-' '\n' \
		| tr "'" '"' \
		> main.go
