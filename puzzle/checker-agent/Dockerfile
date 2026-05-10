FROM golang:1.24-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY checker-agent ./checker-agent
RUN CGO_ENABLED=0 go build -o /out/checker-agent ./checker-agent

FROM alpine:3.20
RUN apk add --no-cache ca-certificates curl
WORKDIR /app
COPY --from=build /out/checker-agent /app/checker-agent
EXPOSE 8082
ENTRYPOINT ["/app/checker-agent"]
