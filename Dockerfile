ARG BASE_IMAGE=registry.opensource.zalan.do/library/alpine-3:latest
FROM ${BASE_IMAGE} as kubectl_download

ARG TARGETARCH

RUN apk add --no-cache --update curl tar coreutils && \
    if [ "${TARGETARCH}" == "" ]; then TARGETARCH=amd64; fi; \
    curl -L -s --fail "https://dl.k8s.io/v1.29.4/kubernetes-client-linux-${TARGETARCH}.tar.gz" -o "kubernetes-client-linux-${TARGETARCH}.tar.gz" && \
    printf "c13235bd929eaaf4d0eaaa9ba883e95ce27a402ca7256c634e20a027fbf72db8834de8ea2ca7238e1fe92859e0edc7384a1cec7fbe2b7a5adf07b2e5cf99b04f kubernetes-client-linux-amd64.tar.gz\n614cd5b5881c583505d089c09c221e4a06da0dc8b5ac70b3d93d7e2a58c8b439446a646d0bb53396c2a48535808503daa6aa1a37f43affe22176c2211fdc2cc4 kubernetes-client-linux-arm64.tar.gz" | grep "${TARGETARCH}" | sha512sum -c - && \
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
