#!/bin/bash
set -e

echo "Building Jules Telegram Bot Setup Tool..."
mkdir -p dist

# Linux AMD64
echo "Building for Linux (amd64)..."
GOOS=linux GOARCH=amd64 go build -o dist/jules-setup-linux-amd64 cmd/setup/main.go

# MacOS AMD64
echo "Building for MacOS (amd64)..."
GOOS=darwin GOARCH=amd64 go build -o dist/jules-setup-darwin-amd64 cmd/setup/main.go

# MacOS ARM64
echo "Building for MacOS (arm64)..."
GOOS=darwin GOARCH=arm64 go build -o dist/jules-setup-darwin-arm64 cmd/setup/main.go

# Windows AMD64
echo "Building for Windows (amd64)..."
GOOS=windows GOARCH=amd64 go build -o dist/jules-setup-windows-amd64.exe cmd/setup/main.go

echo "Build complete! Binaries are in 'dist/' directory."
ls -lh dist/
