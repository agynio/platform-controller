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
    go build -trimpath -ldflags "-s -w" -o /out/provisioning ./cmd/provisioning

# The controller calls the Gateway over HTTPS in installs that terminate TLS
# in-cluster, so the runtime needs a CA bundle.
FROM gcr.io/distroless/base-debian12 AS runtime

WORKDIR /app

COPY --from=builder /out/provisioning /usr/local/bin/provisioning

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/provisioning"]
