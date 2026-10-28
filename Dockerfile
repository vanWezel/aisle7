FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY --from=web /web/dist web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /aisle7 .

# distroless/static ships CA certificates, which the JustWatch calls need.
FROM gcr.io/distroless/static-debian12
COPY --from=build /aisle7 /aisle7
# aisle7 keeps its lists in $XDG_CONFIG_HOME/aisle7; mount that folder to keep them.
ENV XDG_CONFIG_HOME=/data
EXPOSE 8080
ENTRYPOINT ["/aisle7", "serve", "-addr", ":8080"]
