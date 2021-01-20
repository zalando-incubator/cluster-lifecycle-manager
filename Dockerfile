FROM registry.opensource.zalan.do/library/alpine-3.12:latest as kubectl_download

RUN apk add --no-cache --update curl tar coreutils && \
    curl -L -s --fail https://dl.k8s.io/v1.19.7/kubernetes-client-linux-amd64.tar.gz -o kubernetes-client-linux-amd64.tar.gz && \
    echo "8c9606088b91e02bcfe3dd2f26357cd3d2acd1d46b41301cf5247c7e8365e9acacfa3bfecc1865bc7459425beaeb38c8991365ae3c03d3522f1695c2b775ab20 kubernetes-client-linux-amd64.tar.gz" | sha512sum -c - && \
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
