BINARY=seven
BUILD_DIR=build

.PHONY: build test clean

build:
	mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/seven

test:
	go test ./cmd/seven

clean:
	rm -rf $(BUILD_DIR)
