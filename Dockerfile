FROM golang:1.21.0-alpine3.18

RUN mkdir /app/

COPY . /app

WORKDIR /app

RUN apt install ffmpeg

RUN go build -o main ./

CMD ["/app/main"]