# syntax=docker/dockerfile:1

FROM golang:1.24 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bridge .

FROM gcr.io/distroless/static-debian12

COPY --from=builder /bridge /bridge

USER nonroot:nonroot

ENTRYPOINT ["/bridge"]
