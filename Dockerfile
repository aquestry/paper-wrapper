FROM golang:1.25-alpine AS gate-build
WORKDIR /src

COPY gate-src/go.mod gate-src/go.sum ./
RUN go mod download

COPY gate-src/ .
RUN go build -o /out/gate .

FROM alpine:3.21
WORKDIR /mc

RUN apk add --no-cache curl bash openjdk21-jre-headless jq

COPY start.sh .
COPY gate/ gate/
COPY paper/ paper/
COPY --from=gate-build /out/gate gate/gate

RUN chmod +x start.sh gate/gate

EXPOSE 25565

CMD ["./start.sh"]