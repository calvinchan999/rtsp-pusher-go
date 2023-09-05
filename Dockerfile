FROM golang:1.20.7-alpine3.18

RUN apk add --no-cache ffmpeg

WORKDIR /app

COPY . .

RUN go mod download

RUN go build -o main .

CMD ["/app/main"]