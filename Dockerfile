# syntax=docker/dockerfile:1

FROM node:26-trixie-slim AS frontend
WORKDIR /src/frontend
COPY frontend/ ./
RUN ./build.sh

FROM golang:1.27-trixie AS gobuild
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -o /out/console ./cmd/console

FROM gcr.io/distroless/static-debian13
COPY --from=gobuild /out/console /console
EXPOSE 8080
ENTRYPOINT ["/console"]
