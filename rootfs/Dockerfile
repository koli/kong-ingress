FROM quay.io/koli/base-os

RUN adduser --system \
    --shell /bin/bash \
    --disabled-password \
    --no-create-home \
    --group kongc

USER kongc
COPY . /

ENTRYPOINT ["/usr/bin/kong-ingress"]
