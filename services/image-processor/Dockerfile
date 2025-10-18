FROM golang:1.25 AS build

WORKDIR /workspace

COPY go.work go.work
COPY go.work.sum* go.work.sum
COPY .golangci.yml .golangci.yml

COPY libs/go/go.mod libs/go/go.mod
COPY libs/go/go.sum* libs/go/go.sum

COPY libs/go/ libs/go/

COPY services/image-processor/go.mod services/image-processor/go.mod
COPY services/image-processor/go.sum* services/image-processor/go.sum

RUN cd services/image-processor && go mod download

COPY services/image-processor/ services/image-processor/

WORKDIR /workspace/services/image-processor

RUN go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest && \
    golangci-lint run

ARG CGO_ENABLED=0
RUN go build -ldflags="-s -w" -trimpath -o server .

FROM alpine:3.22

RUN apk --no-cache add \
    ca-certificates

WORKDIR /app

COPY --from=build /workspace/services/image-processor/server .

CMD ["./server"]
