FROM golang:alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .


RUN CGO_ENABLED=0 go build -tags memoize_builders -o auto-dns /app/main.go

# Final stage
FROM alpine:latest

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /app/auto-dns .
COPY --from=builder --chown=1000:1000 /app/modsec /app/modsec

# Run the application
ENTRYPOINT ["/app/auto-dns"]