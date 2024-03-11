ARG BASE_IMAGE=registry.opensource.zalan.do/library/alpine-3:latest
FROM ${BASE_IMAGE} as kubectl_download

ARG TARGETARCH

RUN apk add --no-cache --update curl tar coreutils && \
    if [ "${TARGETARCH}" == "" ]; then TARGETARCH=amd64; fi; \
    curl -L -s --fail "https://dl.k8s.io/v1.26.14/kubernetes-client-linux-${TARGETARCH}.tar.gz" -o "kubernetes-client-linux-${TARGETARCH}.tar.gz" && \
    printf "8d49db3e90fbcd08b8391b5d0b60d1b0d56f049686512d3596f7f0e40b48b2e55e98d34cab02ba6daa256a539c86f9dd34cfb2deb117ecde3c713f3cf307bbd6 kubernetes-client-linux-amd64.tar.gz\n6357dfe7881a4fee61bc3c33dc90315b01fee39d0b2e472cea5e5af0ded5d2c32f78aae9e245a8e9d73d042ba797bf4b2aca645e3102e35de8c5fbbea54a8c0b kubernetes-client-linux-arm64.tar.gz" | grep "${TARGETARCH}" | sha512sum -c - && \
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
