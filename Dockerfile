FROM registry.opensource.zalan.do/stups/alpine:latest
LABEL maintainer="Team Teapot @ Zalando SE <team-teapot@zalando.de>"

ARG K8S_VERSION=v1.10.5

# install cluster.py dependencies
# including kubectl
RUN apk add --no-cache python3 ca-certificates openssl git openssh-client && \
    python3 -m ensurepip && \
    rm -r /usr/lib/python*/ensurepip && \
    pip3 install --upgrade stups-senza && \
    wget -O /usr/local/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/$K8S_VERSION/bin/linux/amd64/kubectl && \
    chmod 755 /usr/local/bin/kubectl && \
    rm -rf /var/cache/apk/* /root/.cache /tmp/*

# add binary
ADD build/linux/clm /

CMD ["--help"]
ENTRYPOINT ["/clm"]
