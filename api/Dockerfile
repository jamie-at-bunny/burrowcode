FROM golang:1.25-alpine AS builder
WORKDIR /app
RUN go install github.com/ogen-go/ogen/cmd/ogen@v1.8.1
COPY go.mod openapi.yaml ./
RUN go mod tidy
COPY *.go ./
RUN go generate ./...
RUN go mod tidy
RUN go build -o api .

FROM alpine:3.23
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/api /app/api
COPY --from=builder /app/openapi.yaml /app/openapi.yaml
CMD ["/app/api"]
