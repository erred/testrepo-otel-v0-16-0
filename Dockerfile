FROM golang:rc-alpine

ENV CGO_ENABLED=0
WORKDIR /workspace
COPY go.mod go.sum .
RUN --mount=type=cache,id=gomod,target=/go/pkg/mod \
    go mod download
COPY . .
RUN --mount=type=cache,id=gomod,target=/go/pkg/mod \
    --mount=type=cache,id=gobuild,target=/root/.cache/go-build \
    go build -o /usr/local/bin ./...

FROM scratch
COPY --from=0 /usr/local/bin /bin
