# ==========================================
# 1. BASE (Le socle commun)
# ==========================================
FROM golang:1.25-alpine AS base

# Outils C nécessaires à la compilation CGo (gcc, musl-dev, libwebp-dev)
RUN apk add --no-cache gcc musl-dev libwebp-dev

WORKDIR /app

# Live-reload pour le target dev uniquement
RUN go install github.com/air-verse/air@latest

# Cache partagé des dépendances Go
COPY go.mod go.sum ./
RUN go mod download

# ==========================================
# 2. DEV (L'environnement de développement)
# ==========================================
FROM base AS dev

CMD ["air", "-c", ".air.toml"]

# ==========================================
# 3. BUILDER-WATERMARK (Compilation du microservice Watermark)
# ==========================================
FROM base AS builder-watermark

COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o numan-watermark ./cmd/watermark

# ==========================================
# 4. WATERMARK (Image finale légère non-root UID 10001)
# Build: docker build --target watermark -t <registry>/numan-watermark:<tag> .
# ==========================================
FROM alpine:3.21 AS watermark

RUN apk add --no-cache ca-certificates libwebp \
    && addgroup -g 10001 -S numan \
    && adduser -u 10001 -S -G numan -H -s /sbin/nologin numan

WORKDIR /app

COPY --from=builder-watermark --chown=10001:10001 /app/numan-watermark ./numan-watermark
RUN chmod 0555 /app/numan-watermark

USER 10001:10001
EXPOSE 8080

CMD ["./numan-watermark"]

# ==========================================
# 5. BUILDER-PROD (Compilation de l'API & Worker)
# ==========================================
FROM base AS builder-prod

COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o numan ./cmd/main.go

# ==========================================
# 6. PROD (Image finale légère non-root UID 10001 — API & Worker)
# Build: docker build --target prod -t <registry>/numan-api:<tag> .
# ==========================================
FROM alpine:3.21 AS prod

RUN apk add --no-cache libwebp ca-certificates \
    && addgroup -g 10001 -S numan \
    && adduser -u 10001 -S -G numan -H -s /sbin/nologin numan

WORKDIR /app

COPY --from=builder-prod --chown=10001:10001 /app/numan ./numan
COPY --from=builder-prod --chown=10001:10001 /app/docs ./docs
COPY --from=builder-prod --chown=10001:10001 /app/docs.html ./docs.html
RUN chmod 0555 /app/numan

ENV GIN_MODE=release
USER 10001:10001
EXPOSE 8080 9090

CMD ["./numan"]