FROM golang:1.3

ENV RABBITMQ_USER=admin
ENV RABBITMQ_PASS=password
ENV RABBITMQ_PORT_5672_TCP_ADDR=127.0.0.1
ENV RABBITMQ_PORT_5672_TCP_PORT=5672

EXPOSE 8752

ADD . /go/src/microplatform-io/platform-router-grpc
WORKDIR /go/src/microplatform-io/platform-router-grpc
RUN go get ./...
RUN go install .

ENTRYPOINT ["platform-router-grpc"]
