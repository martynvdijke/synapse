FROM golang:1.26.2-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /synapse-server .

FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata
RUN mkdir -p /db /app

WORKDIR /app
COPY --from=builder /synapse-server .
COPY static/ ./static/

ENV DOCKER=true
ENV DB_PATH=/db/synapse.db
ENV COMPOSE_PATH=/app/docker-compose.yml
ENV LISTEN_ADDR=:6270

EXPOSE 6270
ENTRYPOINT ["./synapse-server"]
