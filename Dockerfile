FROM registry.opensource.zalan.do/library/alpine-3.12:latest as kubectl_download

RUN apk add --no-cache --update curl tar coreutils && \
    curl -L -s --fail https://dl.k8s.io/v1.21.5/kubernetes-client-linux-amd64.tar.gz -o kubernetes-client-linux-amd64.tar.gz && \
    echo "0bd3f5a4141bf3aaf8045a9ec302561bb70f6b9a7d988bc617370620d0dbadef947e1c8855cda0347d1dd1534332ee17a950cac5a8fcb78f2c3e38c62058abde kubernetes-client-linux-amd64.tar.gz" | sha512sum -c - && \
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
