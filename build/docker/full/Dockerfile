ARG image_name=jfrog-cli-full
FROM golang:1.14 as builder
WORKDIR /${image_name}
COPY . /${image_name}
RUN sh build/build.sh
FROM releases-docker.jfrog.io/jfrog-ecosystem-integration-env:latest
ENV CI true
COPY --from=builder /${image_name}/jfrog /usr/local/bin/jfrog
RUN chmod +x /usr/local/bin/jfrog
