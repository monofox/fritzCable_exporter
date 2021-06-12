FROM golang:alpine

WORKDIR /build

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY . .

RUN go build -o fritzcable_exporter .

WORKDIR /dist

RUN cp /build/fritzcable_exporter .

EXPOSE 9623

ENTRYPOINT ["/dist/fritzcable_exporter"]