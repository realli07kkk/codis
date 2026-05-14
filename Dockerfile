FROM golang:1.26.1

RUN apt-get update
RUN apt-get install -y autoconf

ENV GOPATH /gopath
ENV CODIS  ${GOPATH}/src/github.com/CodisLabs/codis
ENV PATH   ${GOPATH}/bin:${PATH}:${CODIS}/bin
ENV GO111MODULE on
COPY . ${CODIS}

RUN make -C ${CODIS} distclean
RUN make -C ${CODIS} build-all

WORKDIR /codis
