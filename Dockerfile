FROM golang:1.21 as builder

WORKDIR /workspace

# Copy go module files first for better caching
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

# Copy source code
COPY main.go main.go
COPY api/ api/
COPY internal/ internal/

# Build the binary
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH:-amd64} go build -a -o manager main.go

FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
