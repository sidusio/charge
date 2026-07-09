FROM alpine AS certs
RUN apk add --no-cache ca-certificates

FROM scratch
ARG TARGETPLATFORM

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY ./build/${TARGETPLATFORM}/charge /charge

ENTRYPOINT [ "/charge" ]
