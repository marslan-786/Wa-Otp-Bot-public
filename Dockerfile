FROM golang:1.24-alpine AS builder

# Enable CGO for SQLite
ENV CGO_ENABLED=1

RUN apk add --no-cache gcc musl-dev git sqlite-dev

WORKDIR /app
COPY . .

RUN go mod init otp-bot || true
RUN go mod tidy
RUN go build -o bot .

FROM alpine:latest
RUN apk add --no-cache ca-certificates sqlite-libs

WORKDIR /app
COPY --from=builder /app/bot .
COPY index.html .
COPY pic.png .
# Create data directory for Volume
RUN mkdir -p /app/data

# Expose Port
EXPOSE 8080

CMD ["./bot"]
