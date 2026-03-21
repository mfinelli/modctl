FROM golang:1.26-alpine AS build
WORKDIR /modctl
RUN apk add gcc libarchive-tools make musl-dev
RUN go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.30.0
RUN go install github.com/google/go-licenses/v2@latest
COPY . /modctl/
RUN make
RUN go-licenses save ./... --ignore github.com/mfinelli/modctl \
  --save_path vendor

FROM alpine
LABEL org.opencontainers.image.title=modctl
LABEL org.opencontainers.image.version=0.4.0
LABEL org.opencontainers.image.description="command line mod manager"
LABEL org.opencontainers.image.url=https://modctl.org
LABEL org.opencontainers.image.source=https://github.com/mfinelli/modctl
LABEL org.opencontainers.image.licenses=GPL-3.0-or-later
RUN apk add libarchive-tools
RUN addgroup -S modctl && adduser -S modctl -G modctl
COPY --from=build /modctl/modctl /usr/bin/modctl
COPY --from=build /modctl/LICENSE /usr/share/licenses/modctl/LICENSE
COPY --from=build /modctl/vendor /usr/share/licenses/modctl/vendor
USER modctl
