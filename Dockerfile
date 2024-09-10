FROM golang:1.23

RUN go install golang.org/x/tools/gopls@latest
