FROM golang:1.21.1-alpine3.17

RUN apk add --no-cache ffmpeg

WORKDIR /app

COPY . .

RUN go mod download

RUN go build -o main .

CMD ["/app/main"]