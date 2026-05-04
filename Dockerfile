FROM alpine:3.23.4
COPY gitspork /usr/local/bin/gitspork
RUN apk update && apk upgrade --no-cache && apk add --no-cache bash git
ENTRYPOINT ["gitspork"]
