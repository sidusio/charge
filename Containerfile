FROM scratch

COPY ./notman /notman

ENTRYPOINT [ "/notman" ]
