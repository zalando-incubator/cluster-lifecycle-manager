FROM registry.opensource.zalan.do/library/alpine-3.12:latest as kubectl_download

RUN apk add --no-cache --update curl tar coreutils && \
    curl -L -s --fail https://dl.k8s.io/v1.18.8/kubernetes-client-linux-amd64.tar.gz -o kubernetes-client-linux-amd64.tar.gz && \
    echo "041dd919f7bf530e6fb6881bc475dbd34cec340eae62193cba1174a0fa0b9d30435b74b0a130db4cdabf35dc59c1bf6bc255e4f91c8ec9a839fb541735e3861e kubernetes-client-linux-amd64.tar.gz" | sha512sum -c - && \
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
