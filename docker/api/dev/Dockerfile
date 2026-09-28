FROM golang:1.27-alpine

WORKDIR /app

# Required for go install from github
RUN apk add --no-cache git

# Install dev tools.
RUN go install github.com/air-verse/air@v1.61.7 \
    && go install github.com/go-delve/delve/cmd/dlv@v1.24.0

# Cache dependencies
COPY go.mod go.sum* ./
RUN go mod download

# Copy source
COPY . .

# Expose app and delve ports
EXPOSE 8080 2345

CMD ["air", "-c", ".air.toml"]