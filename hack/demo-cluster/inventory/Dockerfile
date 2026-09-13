FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /inventory ./hack/demo-cluster/inventory

FROM alpine:latest
COPY --from=builder /inventory /inventory
EXPOSE 8083
ENTRYPOINT ["/inventory"]
