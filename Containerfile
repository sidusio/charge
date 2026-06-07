FROM scratch

COPY ./charge /charge

ENTRYPOINT [ "/charge" ]
