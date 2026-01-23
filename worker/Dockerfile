FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
COPY main.go ./
COPY adapters/ ./adapters/
COPY config/ ./config/
COPY system/ ./system/
RUN go mod download && go build -o worker .

FROM alpine:3.23
RUN apk add --no-cache ffmpeg ca-certificates fontconfig ttf-dejavu
COPY --from=builder /app/worker /worker
CMD ["/worker"]
