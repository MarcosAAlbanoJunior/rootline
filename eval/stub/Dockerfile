FROM golang:1.27.4-alpine AS build
WORKDIR /src
COPY main.go ./
RUN CGO_ENABLED=0 go build -o /stub main.go

FROM scratch
COPY --from=build /stub /stub
ENTRYPOINT ["/stub"]
