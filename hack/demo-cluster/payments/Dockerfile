FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /payments ./hack/demo-cluster/payments

FROM alpine:latest
COPY --from=builder /payments /payments
EXPOSE 8084
ENTRYPOINT ["/payments"]
