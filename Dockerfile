FROM container-registry.zalando.net/library/static:latest
LABEL maintainer="Team Teapot @ Zalando SE <team-teapot@zalando.de>"

ARG TARGETARCH
COPY build/linux/${TARGETARCH}/clm /clm

CMD ["--help"]
ENTRYPOINT ["/clm"]
