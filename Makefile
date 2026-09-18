BINARY := concept2upload
CMD    := ./cmd/concept2upload
DIST   := dist

.PHONY: all build run test vet fmt fmt-check tidy clean install

all: build

build:
	go build -o $(DIST)/$(BINARY) $(CMD)

run: build
	$(DIST)/$(BINARY) $(ARGS)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:" && gofmt -l . && exit 1)

tidy:
	go mod tidy

install:
	go install $(CMD)

clean:
	rm -rf $(DIST)
