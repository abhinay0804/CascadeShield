FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /orders ./hack/demo-cluster/orders

FROM alpine:latest
COPY --from=builder /orders /orders
EXPOSE 8082
ENTRYPOINT ["/orders"]
