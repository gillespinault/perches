FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /perches .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /perches /usr/local/bin/perches
ENV PERCHES_DB=/data/perches.db
EXPOSE 8080
CMD ["perches"]
