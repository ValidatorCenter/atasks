#!/bin/sh

GOARCH=amd64 go build -ldflags "-s" -o atasks_lin64 *.go