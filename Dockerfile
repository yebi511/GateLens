FROM golang:1.24 AS build
ARG GOPROXY=https://goproxy.cn,direct
ARG GOSUMDB=sum.golang.org
ENV GOPROXY=${GOPROXY} GOSUMDB=${GOSUMDB}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/gatelens ./cmd/gatelens

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/gatelens /gatelens
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/gatelens"]

