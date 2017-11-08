FROM golang:1.9.1

RUN mkdir -p /go/src/github.com/alexellis/mailbox/

WORKDIR /go/src/github.com/alexellis/mailbox/

COPY vendor vendor
COPY .  .  

RUN gofmt -l -d $(find . -type f -name '*.go' -not -path "./vendor/*")
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /usr/bin/mailbox .

FROM alpine:3.6
RUN apk --no-cache add ca-certificates
WORKDIR /root/

EXPOSE 8080

ENV http_proxy      ""
ENV https_proxy     ""

COPY --from=0 /usr/bin/mailbox   .

CMD ["./mailbox"]