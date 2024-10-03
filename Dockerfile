FROM golang:1.23

# Install gopls for golang LSP
RUN go install golang.org/x/tools/gopls@latest

# Install delve for debugging
RUN go install github.com/go-delve/delve/cmd/dlv@latest

WORKDIR /app
