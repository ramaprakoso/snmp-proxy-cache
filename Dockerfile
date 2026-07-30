FROM golang:1.21-alpine AS builder

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download || true

COPY . .

RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/snmp-proxy-cache cmd/proxy/main.go

FROM alpine:3.19

WORKDIR /app
COPY --from=builder /app/snmp-proxy-cache /app/snmp-proxy-cache
COPY config/ /app/config/

ENV LISTEN_PORTS="21001,21002,21003,21004,21005"

CMD ["/app/snmp-proxy-cache"]
