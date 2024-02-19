ARG BASE_IMAGE=registry.opensource.zalan.do/library/alpine-3:latest
FROM ${BASE_IMAGE} as kubectl_download

ARG TARGETARCH

RUN apk add --no-cache --update curl tar coreutils && \
    if [ "${TARGETARCH}" == "" ]; then TARGETARCH=amd64; fi; \
    curl -L -s --fail "https://dl.k8s.io/v1.25.16/kubernetes-client-linux-${TARGETARCH}.tar.gz" -o "kubernetes-client-linux-${TARGETARCH}.tar.gz" && \
    printf "887be87d9565ccabde80b92988318ee940d3732e07ebc028a57dda61289fa576760806bc8796fa7a8c41509f8379d3491c30dd2c5a13dca7a56d1fc4ece2aa1e kubernetes-client-linux-amd64.tar.gz\n3ca77c574061bf0fb6fef2f27ce1e17dfbc6aab22d3929e329922528e1d81775be39fc98de9593535d97475168eff6a6cfc886daa257ece7d3b59180a0a9e58f kubernetes-client-linux-arm64.tar.gz" | grep "${TARGETARCH}" | sha512sum -c - && \
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
