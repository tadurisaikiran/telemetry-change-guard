FROM golang:1.26.7-alpine3.23@sha256:b17af760035fc2f338eed92d448a6c67f2d45438844fc6c60678fa5f99e44b57 AS build
WORKDIR /src
COPY go.mod ./
COPY e2e/exporter/main.go ./e2e/exporter/main.go
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tmr-exporter ./e2e/exporter

FROM scratch
COPY --from=build /out/tmr-exporter /tmr-exporter
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/tmr-exporter"]
