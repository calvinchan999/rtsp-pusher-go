FROM golang:1.21.0-alpine3.1

RUN apk add --no-cache ffmpeg

WORKDIR /app

COPY . .

RUN go mod download

RUN go build -o main .

CMD ["/app/main"]