ARG BASE_IMAGE=registry.opensource.zalan.do/library/alpine-3:latest
FROM ${BASE_IMAGE} as kubectl_download

ARG TARGETARCH

RUN apk add --no-cache --update curl tar coreutils && \
    if [ "${TARGETARCH}" == "" ]; then TARGETARCH=amd64; fi; \
    curl -L -s --fail "https://dl.k8s.io/v1.22.17/kubernetes-client-linux-${TARGETARCH}.tar.gz" -o "kubernetes-client-linux-${TARGETARCH}.tar.gz" && \
    printf "fe9fb234653435f75f2de968914b64a1096eceb5014c45d4d1a678b781f3c00aa40420a7421f156daee50350a2b6f91e55a913854bea08d0d0f2c9e3788fe325 kubernetes-client-linux-amd64.tar.gz\n01ee050d537b5e6867fdec635b874e45d5c250ed9d05174024dc4d8bb785173001b2f28fbb0534094a57c0a2bc2f2090040ef1419316963691dc69bbe82c8c37 kubernetes-client-linux-arm64.tar.gz" | grep "${TARGETARCH}" | sha512sum -c - && \
    tar xvf "kubernetes-client-linux-${TARGETARCH}.tar.gz" --strip-components 3 kubernetes/client/bin/ && \
    rm "kubernetes-client-linux-${TARGETARCH}.tar.gz"

FROM ${BASE_IMAGE}
LABEL maintainer="Team Teapot @ Zalando SE <team-teapot@zalando.de>"

ARG TARGETARCH

# install dependencies
RUN apk add --no-cache ca-certificates openssl git openssh-client && \
    rm -rf /var/cache/apk/* /root/.cache /tmp/*

COPY --from=kubectl_download /kubectl /usr/local/bin/

# add binary
ADD build/linux/${TARGETARCH}/clm /

CMD ["--help"]
ENTRYPOINT ["/clm"]
