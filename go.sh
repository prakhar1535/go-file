#!/bin/bash

# Local Go environment variables
export GOROOT="$(pwd)/goroot"
export GOPATH="$(pwd)"
export PATH="$GOROOT/bin:$PATH"

# Run go command with arguments
go "$@" 