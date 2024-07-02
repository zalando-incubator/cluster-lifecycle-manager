ARG BASE_IMAGE=public.ecr.aws/amazonlinux/amazonlinux:2023-minimal
FROM ${BASE_IMAGE} as kubectl_download

ARG TARGETARCH

RUN dnf install -y tar gzip && \
    if [ "${TARGETARCH}" == "" ]; then TARGETARCH=amd64; fi; \
    curl -L -s --fail "https://dl.k8s.io/v1.30.2/kubernetes-client-linux-${TARGETARCH}.tar.gz" -o "kubernetes-client-linux-${TARGETARCH}.tar.gz" && \
    printf "3e3a18138e0436c055322e433398d7ae375e03862cabae71b51883bb78cf969846b9968e426b816e3543c978a4af542e0b292428b00b481d7196e52cf366edbe kubernetes-client-linux-amd64.tar.gz\ncfe9bf3aa4188813607b2c7cad3333dbc1d8a72b1828751261cdd7b21e6ae8c641addd48940bb08cc193ce6901bbf372ad2006e30d0c66b6affbecd5a730b6cf kubernetes-client-linux-arm64.tar.gz" | grep "${TARGETARCH}" | sha512sum -c - && \
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
