FROM alpine:3.23.3
COPY gitspork /usr/local/bin/gitspork
RUN apk update && apk add --no-cache bash git
ENTRYPOINT ["gitspork"]
