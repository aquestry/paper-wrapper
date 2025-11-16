FROM alpine:3.21

WORKDIR /

RUN apk add --no-cache curl bash openjdk21-jre-headless jq

COPY start.sh .
COPY gate/ gate/
COPY paper/ paper/

RUN chmod +x start.sh

EXPOSE 25565

CMD ["./start.sh"]