FROM golang:1-alpine AS builder

WORKDIR /workspace

COPY .golangci.yml .

ARG SERVICE_DIR="services/recordkeeper"
WORKDIR /workspace/${SERVICE_DIR}

COPY ${SERVICE_DIR}/go.mod ${SERVICE_DIR}/go.sum ./

RUN go mod download

WORKDIR /workspace/${SERVICE_DIR}

COPY ${SERVICE_DIR}/ ./

ARG CGO_ENABLED=0
RUN go build \
    -ldflags="-s -w" \
    -trimpath \
    -o server \
    .

FROM scratch

WORKDIR /app

ARG SERVICE_DIR="services/recordkeeper"
COPY --from=builder /workspace/${SERVICE_DIR}/server .

CMD ["./server"]
