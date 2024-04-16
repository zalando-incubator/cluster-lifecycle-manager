ARG BASE_IMAGE=registry.opensource.zalan.do/library/alpine-3:latest
FROM ${BASE_IMAGE} as kubectl_download

ARG TARGETARCH

RUN apk add --no-cache --update curl tar coreutils && \
    if [ "${TARGETARCH}" == "" ]; then TARGETARCH=amd64; fi; \
    curl -L -s --fail "https://dl.k8s.io/v1.28.8/kubernetes-client-linux-${TARGETARCH}.tar.gz" -o "kubernetes-client-linux-${TARGETARCH}.tar.gz" && \
    printf "0812400f8285dc6ead6d25b332ebe3355edd1619b092d1af2445c578553b09a435af7627322eb50d07d0f2d82fd29f4441ceb5ba0b4c4bb990db2ebe197d3e0b kubernetes-client-linux-amd64.tar.gz\n1ae4bd65c4f3483797c9b7dd7e1e8727b22a10dc09c877a9c2d468f615d1cdc915ec9f372832025fa7153cdb8e302d239738671a32a3ec715e2b7aa6d307dbd1 kubernetes-client-linux-arm64.tar.gz" | grep "${TARGETARCH}" | sha512sum -c - && \
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
