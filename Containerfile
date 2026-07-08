ARG BINARY=charge
FROM scratch
ARG BINARY
COPY ./${BINARY} /charge

ENTRYPOINT [ "/charge" ]
