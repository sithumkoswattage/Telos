FROM golang:1.27-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/telos ./cmd/server

# ---- Run stage ----
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

COPY --from=build /out/telos /app/telos
# The server loads these from disk at runtime
COPY templates/ /app/templates/
COPY static/ /app/static/

USER nonroot
EXPOSE 8080
ENTRYPOINT ["/app/telos"]