# Tap API + browser demo in one small static image.
#
#   docker build -t tapd pocs/tap
#   docker run -p 8080:8080 tapd -dev            # in-memory, demo at http://localhost:8080
#
# With Postgres (see deploy/README.md):
#   docker run -p 8080:8080 -e DATABASE_URL=... -e TAP_PHONE_SALT=... \
#     -e TAP_RECEIPT_KEY=... -e TAP_ADMIN_TOKEN=... tapd

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tapd ./backend/cmd/tapd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tapd /tapd
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/tapd"]
