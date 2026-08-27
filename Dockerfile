FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /orchestrateur ./cmd/orchestrateur

FROM alpine:3.20

# Les paquets de l'image de base vieillissent avec le tag auquel elle est épinglée. Cette ligne
# récupère les correctifs de sécurité publiés depuis.
#
# Attention : elle ne se rejoue que si la couche est reconstruite. Le workflow de publication met
# les couches en cache (`cache-from: type=gha`), donc un Dockerfile inchangé pendant des mois
# continue de servir une couche figée. Quand une CVE corrigée apparaît sans changement de code,
# modifier l'instruction elle-même - un commentaire au-dessus ne suffit pas, il ne fait pas partie
# de la commande et n'entre pas dans la clé de cache.
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=builder /orchestrateur .

VOLUME ["/data"]

EXPOSE 8000

ENTRYPOINT ["/app/orchestrateur"]
