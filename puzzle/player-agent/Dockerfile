FROM golang:1.23-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY player-agent ./player-agent
RUN CGO_ENABLED=0 go build -o /out/player-agent ./player-agent

FROM alpine:3.20
RUN apk add --no-cache ca-certificates curl
WORKDIR /app
COPY --from=build /out/player-agent /app/player-agent
EXPOSE 8081
ENTRYPOINT ["/app/player-agent"]
