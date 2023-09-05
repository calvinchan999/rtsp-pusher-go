FROM golang:1.21.0-alpine3.18

RUN apk add --no-cache ffmpeg

WORKDIR /app

COPY . .

RUN go build -o main .

CMD ["/app/main"]