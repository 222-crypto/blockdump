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
	printf "/bin/\n.DS_Store\n/teckit*/" > .gitignore


bin:
	mkdir -p bin


go.mod:
	go mod init $(APP_NAME)


main.go:
	printf "package main--import (-	'fmt'-)--func main() {-	fmt.Println('Hello, World!')-}-" \
		| tr '-' '\n' \
		| tr "'" '"' \
		> main.go


# License obtained through SIL International & eBible.org
teckit.git:
	git clone --bare https://github.com/silnrsi/teckit.git
teckit/license/LICENSING.txt: teckit.git
	git clone teckit.git teckit && cd teckit && git checkout v2.5.12
docs/legal/License_CPLv05.txt: teckit/license/LICENSING.txt
	mkdir -p docs/legal
	cp teckit/license/License_CPLv05.txt docs/legal/License_CPLv05.txt
	diff <(sha512sum docs/legal/License_CPLv05.txt) \
		 <(echo "8b55b2f9bc023153ce9ab7b63accf2c6a1bb8e2cd096f7af16fba65ec20cbfa076a3802612b8912abf63bfa519734b51953c1dd780cdd2053c6d3ae47b64e4ab  docs/legal/License_CPLv05.txt") \
	;
docs/legal/License_LGPLv21.txt: teckit/license/LICENSING.txt
	mkdir -p docs/legal
	cp teckit/license/License_LGPLv21.txt docs/legal/License_LGPLv21.txt
	diff <(sha512sum docs/legal/License_LGPLv21.txt) \
		 <(echo "2360c555e77f97144dc39cc9558dc25d601c2e4c40c81ffce6c4c2c5ec41148ef749d1fbe5a6e536df7f18eeaad0123b08b2f5fe67cc5b52ab7a5bd4868b714e  docs/legal/License_LGPLv21.txt") \
	;
LICENSE: docs/legal/License_CPLv05.txt docs/legal/License_LGPLv21.txt
	curl -s "https://ebible.org/usfx/LICENSING.txt" > LICENSE
	diff <(sha512sum LICENSE) \
		 <(echo "b55c6b627094c0bc50fc43f5be1a7a7f1a3681087bf57cf456f2111646b1360689051434b54aca241878074442522fd99a59207fb56c89388c6b1bd80691aadc  LICENSE") \
	;
docs/LICENSE: LICENSE
	cd docs && ln -s ../LICENSE LICENSE
