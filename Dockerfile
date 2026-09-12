ARG BUILD_FROM

FROM golang:1.26-alpine AS build

ARG VERSION=dev

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /tolk ./cmd/tolk

FROM ${BUILD_FROM}

COPY --from=build /tolk /usr/bin/tolk
COPY run.sh /run.sh
RUN chmod a+x /run.sh

CMD [ "/run.sh" ]
