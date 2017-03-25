FROM quay.io/koli/base:0.1.0

RUN adduser --system \
    --shell /bin/bash \
    --disabled-password \
    --no-create-home \
    --group kongc

USER kongc
COPY . /

ENTRYPOINT ["/usr/bin/kong-ingress"]
