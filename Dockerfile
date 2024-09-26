ARG BASE_IMAGE=public.ecr.aws/amazonlinux/amazonlinux:2023-minimal
FROM ${BASE_IMAGE} as kubectl_download

ARG TARGETARCH

RUN dnf install -y tar gzip && \
    if [ "${TARGETARCH}" == "" ]; then TARGETARCH=amd64; fi; \
    curl -L -s --fail "https://dl.k8s.io/v1.31.1/kubernetes-client-linux-${TARGETARCH}.tar.gz" -o "kubernetes-client-linux-${TARGETARCH}.tar.gz" && \
    printf "609df79769237073275c2a3891e6581c9408da47293276fa12d0332fdef0d2f83bcbf2bea7bb64a9f18b1007ec6500af0ea7daabdcb1aca22d33f4f132a09c27 kubernetes-client-linux-amd64.tar.gz\nd2ac66cc7d48149db5ea17e8262eb1290d542d567a72661000275a24d3fca8c3ea3c8515ae6a19ed5d28e92829a07fb28093853c6ae74b2b946858e967709f09 kubernetes-client-linux-arm64.tar.gz" | grep "${TARGETARCH}" | sha512sum -c - && \
    tar xvf "kubernetes-client-linux-${TARGETARCH}.tar.gz" --strip-components 3 kubernetes/client/bin/ && \
    rm "kubernetes-client-linux-${TARGETARCH}.tar.gz"

FROM ${BASE_IMAGE}
LABEL maintainer="Team Teapot @ Zalando SE <team-teapot@zalando.de>"

ARG TARGETARCH

# install dependencies
RUN dnf install -y openssl git openssh-clients && dnf clean all

COPY --from=kubectl_download /kubectl /usr/local/bin/

# add binary
ADD build/linux/${TARGETARCH}/clm /

CMD ["--help"]
ENTRYPOINT ["/clm"]
