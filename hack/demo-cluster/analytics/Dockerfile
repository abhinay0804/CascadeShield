FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /analytics ./hack/demo-cluster/analytics

FROM alpine:latest
COPY --from=builder /analytics /analytics
EXPOSE 8085
ENTRYPOINT ["/analytics"]
