FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY go.sum ./
RUN go mod download
COPY server ./server
RUN CGO_ENABLED=0 go build -o /venueflow-admin-api ./server
FROM alpine:3.22
COPY --from=build /venueflow-admin-api /venueflow-admin-api
EXPOSE 8080
ENTRYPOINT ["/venueflow-admin-api"]
