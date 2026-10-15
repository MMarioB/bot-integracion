FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /ofertas-bot ./cmd/ofertas-bot

FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=build /ofertas-bot /app/ofertas-bot
COPY config.json /app/config.json
# Monta un volumen en /app/data para conservar el estado entre reinicios
VOLUME ["/app/data"]
ENTRYPOINT ["/app/ofertas-bot"]
CMD ["-interval", "2h"]
