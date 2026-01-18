FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod main.go ./
RUN go mod tidy && go build -o webhooks .

FROM alpine:3.23
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/webhooks /webhooks
CMD ["/webhooks"]
