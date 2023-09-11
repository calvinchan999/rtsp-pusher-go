FROM golang:1.21.0

RUN apt-get update && apt-get install ffmpeg -y

WORKDIR /app

COPY . .

RUN go mod download

RUN go build -o main .

CMD ["/app/main"]