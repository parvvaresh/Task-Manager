# Build stage
FROM golang:1.24 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o taskmanager .

# Final stage
FROM debian:bookworm-slim

WORKDIR /root/

COPY --from=builder /app/taskmanager .

# Run interactively with: docker run -it --rm taskmanager
# Without a TTY it prints the usage; pass a command for one-shot use, e.g. "list".
ENTRYPOINT ["./taskmanager"]
