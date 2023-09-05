FROM golang:1.21.0

RUN apt install ffmpeg

WORKDIR /app

COPY . .

RUN go build -o main .

CMD ["/app/main"]