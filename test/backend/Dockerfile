FROM golang:alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /backend ./test/backend

FROM scratch
COPY --from=builder /backend /backend
ENTRYPOINT ["/backend"]
