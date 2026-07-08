FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN GOEXPERIMENT=jsonv2 CGO_ENABLED=0 GOARCH="$(go env GOARCH)" go build -o /out/charge ./cmd/charge

FROM scratch

COPY --from=build /out/charge /charge

ENTRYPOINT ["/charge"]
