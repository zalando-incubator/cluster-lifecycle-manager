FROM registry.opensource.zalan.do/stups/alpine:latest
MAINTAINER Team Teapot @ Zalando SE <team-teapot@zalando.de>

# install cluster.py dependencies
# including kubectl
RUN apk update && \
    apk add python3 ca-certificates openssl git openssh-client && \
    python3 -m ensurepip && \
    rm -r /usr/lib/python*/ensurepip && \
    pip3 install --upgrade stups-senza && \
    wget -O /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/v1.9.5/bin/linux/amd64/kubectl && \
    chmod 755 /usr/local/bin/kubectl && \
    rm -rf /var/cache/apk/* /root/.cache /tmp/*

# add binary
ADD build/linux/clm /

CMD ["--help"]
ENTRYPOINT ["/clm"]
