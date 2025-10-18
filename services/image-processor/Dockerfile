FROM golang:1.25 AS build

WORKDIR /workspace

COPY .golangci.yml go.work go.work.sum ./

ARG LIBRARY_DIR="libs/go"
WORKDIR /workspace/${LIBRARY_DIR}

COPY ${LIBRARY_DIR}/go.mod ${LIBRARY_DIR}/go.sum ./
COPY ${LIBRARY_DIR}/ ./

ARG SERVICE_DIR="services/image-processor"
WORKDIR /workspace/${SERVICE_DIR}

COPY ${SERVICE_DIR}/go.mod ${SERVICE_DIR}/go.sum ./

RUN go mod download

COPY ${SERVICE_DIR}/ ./

ARG CGO_ENABLED=0
RUN go build -ldflags="-s -w" -trimpath -o server .

FROM alpine:3.22

RUN apk --no-cache add \
    ca-certificates

WORKDIR /app

ARG SERVICE_DIR="services/image-processor"
COPY --from=build /workspace/${SERVICE_DIR}/server .

CMD ["./server"]
