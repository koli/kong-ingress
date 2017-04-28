FROM quay.io/koli/base:v0.2.0

RUN adduser --system \
    --shell /bin/bash \
    --disabled-password \
    --no-create-home \
    --group kongc

USER kongc
COPY . /

ENTRYPOINT ["/usr/bin/kong-ingress"]
