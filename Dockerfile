FROM node:24-alpine AS frontend
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY src/ ./src/
COPY vite.config.js ./
RUN npx vite build

FROM golang:1.26.6-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Copy pre-built frontend assets
COPY --from=frontend /app/static/dist ./static/dist

RUN CGO_ENABLED=1 GOOS=linux go build -o synapse-server .

FROM alpine:latest
RUN apk add --no-cache sqlite-libs ca-certificates

WORKDIR /app

ENV DOCKER=true

COPY --from=builder /app/synapse-server .
COPY --from=builder /app/static ./static

RUN mkdir -p /db && chmod 777 /db

EXPOSE 6270

CMD ["./synapse-server"]
