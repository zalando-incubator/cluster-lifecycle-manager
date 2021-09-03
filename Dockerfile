FROM registry.opensource.zalan.do/library/alpine-3.12:latest as kubectl_download

RUN apk add --no-cache --update curl tar coreutils && \
    curl -L -s --fail https://dl.k8s.io/v1.20.10/kubernetes-client-linux-amd64.tar.gz -o kubernetes-client-linux-amd64.tar.gz && \
    echo "e0505ae0c1e3b59847ba03209ea52c972d2e6c2007e1908a340c3cbc0f81d963ed218c08efdb87809346181ea5133a7df7520774293d9e9adec81b995fb4e45c kubernetes-client-linux-amd64.tar.gz" | sha512sum -c - && \
    tar xvf kubernetes-client-linux-amd64.tar.gz --strip-components 3 kubernetes/client/bin/ && \
    rm kubernetes-client-linux-amd64.tar.gz

FROM registry.opensource.zalan.do/library/alpine-3.12:latest
LABEL maintainer="Team Teapot @ Zalando SE <team-teapot@zalando.de>"

# install cluster.py dependencies
RUN apk add --no-cache python3 ca-certificates openssl git openssh-client && \
    python3 -m ensurepip && \
    rm -r /usr/lib/python*/ensurepip && \
    pip3 install --upgrade stups-senza && \
    rm -rf /var/cache/apk/* /root/.cache /tmp/*

COPY --from=kubectl_download /kubectl /usr/local/bin/

# add binary
ADD build/linux/clm /

CMD ["--help"]
ENTRYPOINT ["/clm"]
