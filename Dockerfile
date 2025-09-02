FROM golang:1.25-alpine AS build

ARG GITSPORK_VERSION=dev

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X 'main.version=${GITSPORK_VERSION}'" -o /tmp/gitspork

FROM alpine:latest AS release
COPY --from=build /tmp/gitspork /usr/local/bin/gitspork
ENTRYPOINT ["gitspork"]