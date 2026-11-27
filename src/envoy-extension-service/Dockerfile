# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/envoy-extension-service .

FROM alpine:3.20
COPY --from=build /out/envoy-extension-service /envoy-extension-service
USER 1000:1000
ENTRYPOINT ["/envoy-extension-service"]
