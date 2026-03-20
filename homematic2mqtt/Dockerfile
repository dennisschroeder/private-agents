FROM golang:alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o homematic2mqtt .

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/homematic2mqtt .
EXPOSE 8081
ENTRYPOINT ["./homematic2mqtt"]
