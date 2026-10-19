# Licensed to the Apache Software Foundation (ASF) under one
# or more contributor license agreements.  See the NOTICE file
# distributed with this work for additional information
# regarding copyright ownership.  The ASF licenses this file
# to you under the Apache License, Version 2.0 (the
# "License"); you may not use this file except in compliance
# with the License.  You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.

# The new front end (frontend/web, the chat) is built in a stage of its own;
# only its dist/ reaches the image, where the Go server serves it under /app.
# pnpm comes from corepack at the version frontend/package.json pins.
FROM node:22-alpine AS web-builder
WORKDIR /src
COPY frontend/ ./
RUN corepack enable     && pnpm install --frozen-lockfile --filter web...     && pnpm --filter web build

FROM golang:1.25-alpine AS golang-builder
LABEL maintainer="linkinstar@apache.org"

ARG GOPROXY
# ENV GOPROXY ${GOPROXY:-direct}
# ENV GOPROXY=https://proxy.golang.com.cn,direct

ENV GOPATH /go
ENV GOROOT /usr/local/go
ENV PACKAGE github.com/apache/answer
ENV BUILD_DIR ${GOPATH}/src/${PACKAGE}
ENV ANSWER_MODULE ${BUILD_DIR}

ARG TAGS="sqlite sqlite_unlock_notify"
ENV TAGS "bindata timetzdata $TAGS"
ARG CGO_EXTRA_CFLAGS

COPY . ${BUILD_DIR}
WORKDIR ${BUILD_DIR}
RUN apk --no-cache add build-base git bash nodejs npm && npm install -g pnpm@9.7.0 \
    && make clean build

RUN chmod 755 answer
RUN ["/bin/bash","-c","script/build_plugin.sh"]
RUN cp answer /usr/bin/answer

RUN mkdir -p /data/uploads && chmod 777 /data/uploads \
    && mkdir -p /data/i18n && cp -r i18n/*.yaml /data/i18n

FROM alpine
LABEL maintainer="linkinstar@apache.org"

ARG TIMEZONE
ENV TIMEZONE=${TIMEZONE:-"Asia/Shanghai"}

RUN apk update \
    && apk --no-cache add \
        bash \
        ca-certificates \
        curl \
        dumb-init \
        gettext \
        openssh \
        sqlite \
        gnupg \
        tzdata \
    && ln -sf /usr/share/zoneinfo/${TIMEZONE} /etc/localtime \
    && echo "${TIMEZONE}" > /etc/timezone

COPY --from=golang-builder /usr/bin/answer /usr/bin/answer
COPY --from=golang-builder /data /data
# internal/router/web.go looks for the new front end here
COPY --from=web-builder /src/web/dist /usr/share/maibumai/web
COPY /script/entrypoint.sh /entrypoint.sh
RUN chmod 755 /entrypoint.sh

VOLUME /data
EXPOSE 80
# dumb-init runs as PID 1: the kernel drops unhandled signals sent to PID 1, so
# without it docker stop's SIGTERM never reaches answer and it gets SIGKILLed.
ENTRYPOINT ["dumb-init","--","/entrypoint.sh"]
