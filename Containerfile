FROM scratch
ARG TARGETPLATFORM

COPY ./build/${TARGETPLATFORM}/charge /charge

ENTRYPOINT [ "/charge" ]
