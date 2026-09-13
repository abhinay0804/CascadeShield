FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /auth ./hack/demo-cluster/auth

FROM alpine:latest
COPY --from=builder /auth /auth
EXPOSE 8081
ENTRYPOINT ["/auth"]
