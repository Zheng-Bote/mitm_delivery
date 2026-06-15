#!/usr/bin/sh

MITM_VERSION=$(git describe --tags)

go build -ldflags "-X main.version=${MITM_VERSION}" -o ./bin/mitm-deliver ./cmd/deliver/main.go

cp bin/mitm-deliver ../../scheduler/mitm_scheduler/bin/.