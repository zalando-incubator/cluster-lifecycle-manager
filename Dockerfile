FROM registry.opensource.zalan.do/stups/alpine:latest as kubectl_download

RUN apk add --no-cache --update curl tar coreutils && \
    curl -L -s --fail https://dl.k8s.io/v1.15.6/kubernetes-client-linux-amd64.tar.gz -o kubernetes-client-linux-amd64.tar.gz && \
    echo "32292b73d40c01a55e9d820c8a2d69f7ae68efd86954eb25a824bc4730146fe174d5a0ea9eb4bc914f9e725a59f8aab411138cb09ec87e1cec130dd16eb46273 kubernetes-client-linux-amd64.tar.gz" | sha512sum -c - && \
    tar xvf kubernetes-client-linux-amd64.tar.gz --strip-components 3 kubernetes/client/bin/ && \
    rm kubernetes-client-linux-amd64.tar.gz

FROM registry.opensource.zalan.do/stups/alpine:latest
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
