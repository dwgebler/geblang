FROM golang:1.26.3-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN GOTOOLCHAIN=local CGO_ENABLED=0 go build -ldflags=-funcalign=64 -o /out/geblang ./cmd/geblang
RUN mkdir -p /out/stdlib && cp -R stdlib/. /out/stdlib/

FROM scratch AS artifacts
COPY --from=build /out /out
CMD ["/out/geblang", "--version"]
