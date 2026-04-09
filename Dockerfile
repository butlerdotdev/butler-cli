# Copyright 2026 The Butler Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Stage 1: Build
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w \
      -X github.com/butlerdotdev/butler/internal/common/version.Version=${VERSION} \
      -X github.com/butlerdotdev/butler/internal/common/version.Commit=${COMMIT} \
      -X github.com/butlerdotdev/butler/internal/common/version.Date=${BUILD_DATE}" \
    -o /out/butlerctl ./cmd/butlerctl

RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w \
      -X github.com/butlerdotdev/butler/internal/common/version.Version=${VERSION} \
      -X github.com/butlerdotdev/butler/internal/common/version.Commit=${COMMIT} \
      -X github.com/butlerdotdev/butler/internal/common/version.Date=${BUILD_DATE}" \
    -o /out/butleradm ./cmd/butleradm

# Stage 2: Runtime
FROM alpine:3.21

RUN apk add --no-cache ca-certificates

COPY --from=builder /out/butlerctl /usr/local/bin/butlerctl
COPY --from=builder /out/butleradm /usr/local/bin/butleradm

ENTRYPOINT ["/usr/local/bin/butlerctl"]
