FROM golang:1.25-alpine AS build

COPY . .
RUN CGO_ENABLED=0 go build -o /tmp/gitspork

FROM scratch AS release
COPY --from=build /tmp/gitspork /usr/local/bin/gitspork
ENTRYPOINT ["gitspork"]