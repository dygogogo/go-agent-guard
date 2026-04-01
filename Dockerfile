FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o mcp-guard .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/mcp-guard /usr/local/bin/mcp-guard

EXPOSE 8080

ENTRYPOINT ["mcp-guard"]
