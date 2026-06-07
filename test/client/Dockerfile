FROM golang:alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /client ./test/client

FROM scratch
COPY --from=builder /client /client
ENTRYPOINT ["/client"]
