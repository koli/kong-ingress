FROM quay.io/koli/golang:1.7.5-alpine3.5

RUN apk add --no-cache git make curl

ENV SHORTNAME kong-ingress
ENV REPO_DIR /go/src/kolihub.io/${SHORTNAME}
RUN mkdir -p ${REPO_DIR}
WORKDIR /go/src/kolihub.io/${SHORTNAME}
ADD . /go/src/kolihub.io/${SHORTNAME}        

ENV GOPATH /go

RUN curl --progress-bar -L https://s3.amazonaws.com/koli-vendors/vendor-kong-ingress.tar.gz | tar -xz -C ${REPO_DIR}

RUN make build-local

RUN cp -a ${REPO_DIR}/rootfs/* /

RUN adduser -s /bin/sh -D  kongc
WORKDIR /home/kongc

# Clean Image
RUN apk del --force --purge curl git make ca-certificates libc-utils musl-utils scanelf
RUN rm -Rf /go \
    && rm -Rf /usr/local/go


USER kongc

ENTRYPOINT ["/usr/bin/kong-ingress"]
