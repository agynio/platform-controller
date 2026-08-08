# syntax=docker/dockerfile:1.8

FROM golang:1.25 AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w" -o /out/platform-controller ./cmd/platform-controller

# The controller calls the Gateway over HTTPS in installs that terminate TLS
# in-cluster, so the runtime needs a CA bundle.
FROM gcr.io/distroless/base-debian12 AS runtime

WORKDIR /app

COPY --from=builder /out/platform-controller /usr/local/bin/platform-controller

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/platform-controller"]
