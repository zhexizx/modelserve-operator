FROM golang:1.26 AS builder
LABEL authors="Zhexi"

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY cmd/ cmd/
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /model-server ./cmd/model-server

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /model-server /model-server

COPY models/ /models/

ENV MODEL_PATH=/models/keywords-v1.txt \
    PORT=8080
EXPOSE 8080

USER nonroot:nonroot
ENTRYPOINT ["/model-server"]
