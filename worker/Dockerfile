FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod main.go ./
RUN go mod tidy && go build -o worker .

FROM alpine:3.23
RUN apk add --no-cache ffmpeg ca-certificates fontconfig ttf-dejavu
COPY --from=builder /app/worker /worker
CMD ["/worker"]
