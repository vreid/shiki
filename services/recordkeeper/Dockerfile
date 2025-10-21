FROM golang:1.25-alpine AS build

WORKDIR /workspace

COPY .golangci.yml .

ARG LIBRARY_DIR="libs/go/types"
WORKDIR /workspace/${LIBRARY_DIR}

COPY ${LIBRARY_DIR}/go.mod ${LIBRARY_DIR}/go.sum ./
COPY ${LIBRARY_DIR}/ ./

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
COPY --from=build /workspace/${SERVICE_DIR}/server .

CMD ["./server"]
