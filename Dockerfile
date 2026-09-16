FROM golang:1.23-bookworm AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /map-and-zip ./cmd/map-and-zip

FROM gcr.io/distroless/static-debian12

COPY --from=build /map-and-zip /map-and-zip

ENTRYPOINT ["/map-and-zip"]
