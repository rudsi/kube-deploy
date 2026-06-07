# Build stage
FROM golang:1.22-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /kube-deploy .

# Runtime stage (distroless, non-root — matches VIBSL hardened images)
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /kube-deploy /kube-deploy

ENV HOST=0.0.0.0
ENV PORT=8080
# Default for VIBSL when platform env injection fails; override in dashboard with a real secret.
ENV API_TOKEN=dev-token

EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/kube-deploy"]
