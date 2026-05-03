FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/openlimit-gateway ./cmd/gateway

FROM alpine:3.20
RUN adduser -D -H openlimit
WORKDIR /app
COPY --from=builder /out/openlimit-gateway /usr/local/bin/openlimit-gateway
COPY configs/gateway.example.yaml /app/configs/gateway.yaml
USER openlimit
EXPOSE 8080
ENTRYPOINT ["openlimit-gateway"]
